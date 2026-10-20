package apply

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"mktree/internal/tree"
)

type Action int

const (
	Create Action = iota
	Exists
	Overwrite
	Conflict
)

func (a Action) String() string {
	return [...]string{"+", "=", "~", "!"}[a]
}

type Step struct {
	// slash-separated and relative to the plan root, with a trailing slash for dirs
	Path   string
	Action Action
	Reason string
	Node   *tree.Node
}

func (s Step) String() string {
	if s.Reason != "" {
		return fmt.Sprintf("%s %s   %s", s.Action, s.Path, s.Reason)
	}
	return fmt.Sprintf("%s %s", s.Action, s.Path)
}

type Options struct {
	// overwrite files whose content differs
	Force bool
}

// Plan is the ordered list of steps that makes Root match a tree. Parents come before children.
type Plan struct {
	Root  string
	Steps []Step
}

var ErrConflicts = errors.New("plan has conflicts")

// New compares nodes against the directory root without writing anything.
func New(root string, nodes []*tree.Node, opts Options) (*Plan, error) {
	if err := tree.Validate(nodes); err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s: not a directory", root)
	}
	p := &Plan{Root: root}
	if err := p.add(nodes, "", opts); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Plan) add(nodes []*tree.Node, dir string, opts Options) error {
	for _, n := range nodes {
		rel := dir + n.Name
		s := Step{Path: rel, Node: n}
		if n.Type == tree.Dir {
			s.Path += "/"
		}

		info, err := os.Lstat(p.path(rel))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			s.Action = Create
		case err != nil:
			return err
		default:
			s.Action, s.Reason, err = compare(n, p.path(rel), info, opts)
			if err != nil {
				return err
			}
		}
		p.Steps = append(p.Steps, s)

		if n.Type == tree.Dir && s.Action != Conflict {
			if err := p.add(n.Children, rel+"/", opts); err != nil {
				return err
			}
		}
	}
	return nil
}

func compare(n *tree.Node, path string, info fs.FileInfo, opts Options) (Action, string, error) {
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return Conflict, "is a symlink", nil
	case n.Type == tree.Dir && info.IsDir():
		return Exists, "", nil
	case n.Type == tree.Dir:
		return Conflict, "exists and is not a directory", nil
	case info.IsDir():
		return Conflict, "exists as a directory", nil
	case !info.Mode().IsRegular():
		return Conflict, "exists and is not a regular file", nil
	case n.Content == nil:
		return Exists, "", nil
	}

	b, err := os.ReadFile(path)
	switch {
	case err != nil:
		return 0, "", err
	case string(b) == *n.Content:
		return Exists, "", nil
	case opts.Force:
		return Overwrite, "", nil
	}
	return Conflict, "content differs", nil
}

func (p *Plan) Conflicts() []Step {
	var out []Step
	for _, s := range p.Steps {
		if s.Action == Conflict {
			out = append(out, s)
		}
	}
	return out
}

// Changed reports whether applying the plan would write anything.
func (p *Plan) Changed() bool {
	for _, s := range p.Steps {
		if s.Action == Create || s.Action == Overwrite {
			return true
		}
	}
	return false
}

// Apply executes the plan. It writes nothing if the plan has conflicts.
func (p *Plan) Apply() error {
	if len(p.Conflicts()) > 0 {
		return ErrConflicts
	}
	for _, s := range p.Steps {
		path := p.path(s.Path)
		var err error
		switch {
		case s.Action == Create && s.Node.Type == tree.Dir:
			err = os.Mkdir(path, 0o755)
		case s.Action == Create:
			err = createFile(path, s.Node.Content)
		case s.Action == Overwrite:
			err = os.WriteFile(path, []byte(*s.Node.Content), 0o644)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Plan) path(rel string) string {
	return filepath.Join(p.Root, filepath.FromSlash(rel))
}

// createFile fails if path already exists, so a file created since planning is never clobbered.
func createFile(path string, content *string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if content != nil {
		if _, err := f.WriteString(*content); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}
