package apply

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mktree/internal/tree"
)

func plan(t *testing.T, root, src string, opts Options) *Plan {
	t.Helper()
	nodes, err := tree.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(root, nodes, opts)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func assertSteps(t *testing.T, p *Plan, want string) {
	t.Helper()
	var lines []string
	for _, s := range p.Steps {
		lines = append(lines, s.String())
	}
	if got := strings.Join(lines, "\n"); got != strings.TrimSpace(want) {
		t.Errorf("got\n%s\nwant\n%s", got, strings.TrimSpace(want))
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != want {
		t.Errorf("%s: got %q, want %q", path, b, want)
	}
}

const project = `
project/
  docs/
    a.md
      hello
  Makefile
`

func TestApplyFresh(t *testing.T) {
	root := t.TempDir()
	p := plan(t, root, project, Options{})
	assertSteps(t, p, `
+ project/
+ project/docs/
+ project/docs/a.md
+ project/Makefile
`)
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(root, "project/docs/a.md"), "hello\n")
	assertFile(t, filepath.Join(root, "project/Makefile"), "")

	again := plan(t, root, project, Options{})
	if again.Changed() {
		t.Error("second plan should be a no-op")
	}
	assertSteps(t, again, `
= project/
= project/docs/
= project/docs/a.md
= project/Makefile
`)
}

func TestApplyMerge(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "project/Makefile"), "keep me\n")
	write(t, filepath.Join(root, "project/docs/a.md"), "hello\n")
	write(t, filepath.Join(root, "project/untracked"), "")

	p := plan(t, root, project, Options{})
	assertSteps(t, p, `
= project/
= project/docs/
= project/docs/a.md
= project/Makefile
`)
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(root, "project/Makefile"), "keep me\n")
	assertFile(t, filepath.Join(root, "project/untracked"), "")
}

func TestApplyConflicts(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.md"), "old\n")
	write(t, filepath.Join(root, "docs"), "")
	write(t, filepath.Join(root, "src/main.go"), "")
	if err := os.Symlink("a.md", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	src := `
a.md
  new
docs/
  inner.md
src
link
new.txt
`
	p := plan(t, root, src, Options{})
	assertSteps(t, p, `
! a.md   content differs
! docs/   exists and is not a directory
! src   exists as a directory
! link   is a symlink
+ new.txt
`)
	if err := p.Apply(); !errors.Is(err, ErrConflicts) {
		t.Fatalf("got %v, want ErrConflicts", err)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("nothing should be written when there are conflicts")
	}

	// force only resolves content conflicts
	forced := plan(t, root, src, Options{Force: true})
	if got := len(forced.Conflicts()); got != 3 {
		t.Errorf("got %d conflicts with force, want 3", got)
	}

	p = plan(t, root, "a.md\n  new\n", Options{Force: true})
	assertSteps(t, p, "~ a.md")
	if err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(root, "a.md"), "new\n")
}

func TestNewErrors(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "file"), "")

	if _, err := New(filepath.Join(root, "missing"), nil, Options{}); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing root: got %v", err)
	}
	if _, err := New(filepath.Join(root, "file"), nil, Options{}); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("file root: got %v", err)
	}
	bad := []*tree.Node{{Name: "..", Type: tree.Dir}}
	if _, err := New(root, bad, Options{}); err == nil || !strings.Contains(err.Error(), "invalid name") {
		t.Errorf("invalid tree: got %v", err)
	}
}
