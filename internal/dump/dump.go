package dump

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"mktree/internal/tree"
)

type Options struct {
	// include bodies of text files up to MaxSize bytes
	Contents bool
	MaxSize  int64
	Hidden   bool
	// glob patterns matched against base names
	Ignore    []string
	Gitignore bool
}

// Dir reads the tree under root, sorted by name with directories first.
// Symlinks and other non-regular files are skipped, and .git is never included.
func Dir(root string, opts Options) ([]*tree.Node, error) {
	for _, p := range opts.Ignore {
		if _, err := filepath.Match(p, ""); err != nil {
			return nil, fmt.Errorf("-I %q: %w", p, err)
		}
	}
	w := &walker{root: root, opts: opts}
	if opts.Gitignore {
		var err error
		if w.ignored, err = gitIgnored(root); err != nil {
			return nil, err
		}
	}
	return w.dir("")
}

type walker struct {
	root    string
	opts    Options
	ignored map[string]bool
}

func (w *walker) dir(rel string) ([]*tree.Node, error) {
	entries, err := os.ReadDir(w.path(rel))
	if err != nil {
		return nil, err
	}
	var dirs, files []*tree.Node
	for _, e := range entries {
		name, path := e.Name(), rel+e.Name()
		switch {
		case e.IsDir():
			if w.skip(name, path+"/") {
				continue
			}
			children, err := w.dir(path + "/")
			if err != nil {
				return nil, err
			}
			dirs = append(dirs, &tree.Node{Name: name, Type: tree.Dir, Children: children})
		case e.Type().IsRegular():
			if w.skip(name, path) {
				continue
			}
			n := &tree.Node{Name: name, Type: tree.File}
			if w.opts.Contents {
				if n.Content, err = w.content(e, path); err != nil {
					return nil, err
				}
			}
			files = append(files, n)
		}
	}
	return append(dirs, files...), nil
}

func (w *walker) skip(name, rel string) bool {
	if name == ".git" || !w.opts.Hidden && strings.HasPrefix(name, ".") || w.ignored[rel] {
		return true
	}
	for _, p := range w.opts.Ignore {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}

// content returns nil for binary and oversized files.
func (w *walker) content(e fs.DirEntry, rel string) (*string, error) {
	info, err := e.Info()
	if err != nil {
		return nil, err
	}
	if info.Size() > w.opts.MaxSize {
		return nil, nil
	}
	b, err := os.ReadFile(w.path(rel))
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return nil, nil
	}
	s := string(b)
	return &s, nil
}

func (w *walker) path(rel string) string {
	return filepath.Join(w.root, filepath.FromSlash(rel))
}

// gitIgnored lists ignored paths under root, with a trailing slash for whole directories.
func gitIgnored(root string) (map[string]bool, error) {
	out, err := exec.Command("git", "-C", root, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			err = errors.New(strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("--gitignore: %w", err)
	}
	ignored := make(map[string]bool)
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			ignored[p] = true
		}
	}
	return ignored, nil
}
