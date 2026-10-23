package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"mktree/internal/apply"
	"mktree/internal/dump"
	"mktree/internal/tree"
)

const example = `project/  # trailing / makes a directory
  src/main.go  # a/b shorthand creates parents
    package main

    func main() {}
  data/
  .gitignore
`

//go:embed docs/howto.md
var howto string

var usage = `usage:
  mktree [flags] [TARGET]      apply a tree to TARGET (default .)
  mktree dump [flags] [DIR]    print DIR as a tree (default .)
  mktree convert [flags]       convert between tree, json and yaml

Input is read from -f FILE, or from stdin when -f is omitted or -.
Flags go before TARGET or DIR.
Full guide to the format and behavior: mktree --howto

example:
  mktree <<'EOF'
` + indent(example, "  ") + `  EOF

Indent with spaces; the first indented line sets the unit. Lines one level
under a file are its content, kept as-is including # and tabs. A file with
no content is created empty, or left alone if it exists. Existing files are
never changed without --force (--no-clobber keeps them quietly). To see how input parses: mktree convert.

apply:
  -f FILE         input file
  --from FMT      tree, json or yaml (default: by extension, else tree)
  -n, --dry-run   print the plan, write nothing
  --force         overwrite files whose content differs
  --no-clobber    keep files whose content differs instead of conflicting
  -q              print only conflicts
  -v              also print entries that already exist
  --howto         print the full guide

dump:
  --to FMT        tree, json or yaml (default tree)
  --contents      include bodies of text files
  --max-size N    largest body to include, in bytes (default 65536)
  --hidden        include dotfiles
  -I PATTERN      skip names matching a glob (repeatable)
  --gitignore     skip files ignored by git

convert:
  -f FILE, --from FMT, --to FMT

Plan: + create, = exists, ~ overwrite, ! conflict
Exit: 0 success, 1 conflicts or I/O error, 2 parse or usage error
`

func indent(s, prefix string) string {
	lines := strings.SplitAfter(s, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "")
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

type cli struct {
	stdin          io.Reader
	stdout, stderr io.Writer
}

type usageError string

func (e usageError) Error() string { return string(e) }

// inputError is a problem with the document being read.
type inputError struct {
	name string
	err  error
}

func (e *inputError) Error() string { return e.name + ":" + sep(e.err) + e.err.Error() }

// sep keeps tree errors in file:line:col form.
func sep(err error) string {
	var te *tree.Error
	if errors.As(err, &te) {
		return ""
	}
	return " "
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	c := &cli{stdin, stdout, stderr}
	cmd := c.apply
	if len(args) > 0 {
		switch args[0] {
		case "dump":
			cmd, args = c.dump, args[1:]
		case "convert":
			cmd, args = c.convert, args[1:]
		}
	}

	err := cmd(args)
	var ue usageError
	var ie *inputError
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprint(stdout, usage)
		return 0
	case errors.As(err, &ue):
		fmt.Fprintf(stderr, "mktree: %v\nrun 'mktree -h' for usage\n", err)
		return 2
	case errors.As(err, &ie):
		fmt.Fprintf(stderr, "mktree: %v\n", err)
		return 2
	}
	fmt.Fprintf(stderr, "mktree: %v\n", err)
	return 1
}

func (c *cli) apply(args []string) error {
	fs := newFlags()
	file := fs.String("f", "", "")
	from := fs.String("from", "", "")
	var dryRun, force, noClobber, quiet, verbose bool
	fs.BoolVar(&dryRun, "n", false, "")
	fs.BoolVar(&dryRun, "dry-run", false, "")
	fs.BoolVar(&force, "force", false, "")
	fs.BoolVar(&noClobber, "no-clobber", false, "")
	fs.BoolVar(&quiet, "q", false, "")
	fs.BoolVar(&verbose, "v", false, "")
	showHowto := fs.Bool("howto", false, "")
	if err := parse(fs, args, 1); err != nil {
		return err
	}
	if force && noClobber {
		return usageError("--force and --no-clobber are mutually exclusive")
	}
	if *showHowto {
		_, err := io.WriteString(c.stdout, howto)
		return err
	}
	target := "."
	if fs.NArg() == 1 {
		target = fs.Arg(0)
	}

	nodes, err := c.read(*file, *from)
	if err != nil {
		return err
	}
	p, err := apply.New(target, nodes, apply.Options{Force: force, NoClobber: noClobber})
	if err != nil {
		return err
	}
	for _, s := range p.Steps {
		switch s.Action {
		case apply.Conflict:
		case apply.Exists:
			if !verbose && (quiet || s.Reason == "") {
				continue
			}
		default:
			if quiet {
				continue
			}
		}
		fmt.Fprintln(c.stdout, s)
	}

	if n := len(p.Conflicts()); n > 0 {
		if n == 1 {
			return errors.New("1 conflict, nothing written")
		}
		return fmt.Errorf("%d conflicts, nothing written", n)
	}
	if dryRun {
		return nil
	}
	return p.Apply()
}

func (c *cli) dump(args []string) error {
	fs := newFlags()
	to := fs.String("to", "tree", "")
	var opts dump.Options
	fs.BoolVar(&opts.Contents, "contents", false, "")
	fs.Int64Var(&opts.MaxSize, "max-size", 64<<10, "")
	fs.BoolVar(&opts.Hidden, "hidden", false, "")
	fs.Var((*patterns)(&opts.Ignore), "I", "")
	fs.BoolVar(&opts.Gitignore, "gitignore", false, "")
	if err := parse(fs, args, 1); err != nil {
		return err
	}
	if err := checkFormat(*to); err != nil {
		return err
	}
	dir := "."
	if fs.NArg() == 1 {
		dir = fs.Arg(0)
	}

	nodes, err := dump.Dir(dir, opts)
	if err != nil {
		return err
	}
	if *to == "tree" {
		c.dropInexpressible(nodes, "")
	}
	return encode(c.stdout, nodes, *to)
}

// dropInexpressible removes bodies that the tree format would change, so a dump never misstates a file.
func (c *cli) dropInexpressible(nodes []*tree.Node, dir string) {
	for _, n := range nodes {
		switch {
		case n.Type == tree.Dir:
			c.dropInexpressible(n.Children, dir+n.Name+"/")
		case n.Content != nil && *n.Content != "" && !tree.Expressible(*n.Content):
			fmt.Fprintf(c.stderr, "mktree: %s%s: content omitted, the tree format cannot express it exactly (use --to json)\n", dir, n.Name)
			n.Content = nil
		}
	}
}

func (c *cli) convert(args []string) error {
	fs := newFlags()
	file := fs.String("f", "", "")
	from := fs.String("from", "", "")
	to := fs.String("to", "tree", "")
	if err := parse(fs, args, 0); err != nil {
		return err
	}
	if err := checkFormat(*to); err != nil {
		return err
	}
	nodes, err := c.read(*file, *from)
	if err != nil {
		return err
	}
	return encode(c.stdout, nodes, *to)
}

func newFlags() *flag.FlagSet {
	fs := flag.NewFlagSet("mktree", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parse(fs *flag.FlagSet, args []string, maxArgs int) error {
	err := fs.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		return err
	case err != nil:
		return usageError(err.Error())
	case fs.NArg() > maxArgs:
		return usageError(fmt.Sprintf("unexpected argument %q", fs.Arg(maxArgs)))
	}
	return nil
}

type patterns []string

func (p *patterns) String() string { return strings.Join(*p, ",") }

func (p *patterns) Set(s string) error {
	*p = append(*p, s)
	return nil
}

func checkFormat(f string) error {
	switch f {
	case "tree", "json", "yaml":
		return nil
	}
	return usageError(fmt.Sprintf("unknown format %q (want tree, json or yaml)", f))
}

func (c *cli) read(file, from string) ([]*tree.Node, error) {
	format := from
	if format == "" {
		switch filepath.Ext(file) {
		case ".json":
			format = "json"
		case ".yaml", ".yml":
			format = "yaml"
		default:
			format = "tree"
		}
	}
	if err := checkFormat(format); err != nil {
		return nil, err
	}

	var b []byte
	var err error
	name := file
	if file == "" || file == "-" {
		name = "<stdin>"
		if f, ok := c.stdin.(*os.File); ok && isTerminal(f) {
			return nil, usageError("no input: pass -f FILE or pipe a tree on stdin")
		}
		b, err = io.ReadAll(c.stdin)
	} else {
		b, err = os.ReadFile(file)
	}
	if err != nil {
		return nil, err
	}

	nodes, err := decode(b, format)
	if err != nil {
		return nil, &inputError{name, err}
	}
	return nodes, nil
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func decode(b []byte, format string) ([]*tree.Node, error) {
	var nodes []*tree.Node
	var err error
	switch format {
	case "tree":
		return tree.Parse(b)
	case "json":
		d := json.NewDecoder(bytes.NewReader(b))
		d.DisallowUnknownFields()
		err = d.Decode(&nodes)
	case "yaml":
		d := yaml.NewDecoder(bytes.NewReader(b))
		d.KnownFields(true)
		err = d.Decode(&nodes)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return nodes, tree.Validate(nodes)
}

func encode(w io.Writer, nodes []*tree.Node, format string) error {
	if nodes == nil {
		nodes = []*tree.Node{}
	}
	switch format {
	case "json":
		e := json.NewEncoder(w)
		e.SetEscapeHTML(false)
		e.SetIndent("", "  ")
		return e.Encode(nodes)
	case "yaml":
		e := yaml.NewEncoder(w)
		e.SetIndent(2)
		if err := e.Encode(nodes); err != nil {
			return err
		}
		return e.Close()
	}
	b, err := tree.Format(nodes)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
