package dump

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"mktree/internal/tree"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func format(t *testing.T, root string, opts Options) string {
	t.Helper()
	nodes, err := Dir(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	out, err := tree.Format(nodes)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func fixture(t *testing.T) string {
	root := t.TempDir()
	write(t, filepath.Join(root, "b.txt"), "hello\n")
	write(t, filepath.Join(root, "a/z.go"), "package z\n")
	write(t, filepath.Join(root, "a/bin"), "\x00\x01")
	write(t, filepath.Join(root, "big"), "0123456789\n")
	write(t, filepath.Join(root, ".env"), "SECRET=1\n")
	write(t, filepath.Join(root, ".git/HEAD"), "ref\n")
	write(t, filepath.Join(root, "node_modules/x.js"), "")
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("b.txt", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDir(t *testing.T) {
	root := fixture(t)

	got := format(t, root, Options{})
	want := `a/
  bin
  z.go
empty/
node_modules/
  x.js
b.txt
big
`
	if got != want {
		t.Errorf("default:\ngot\n%s\nwant\n%s", got, want)
	}

	got = format(t, root, Options{Contents: true, MaxSize: 10, Hidden: true, Ignore: []string{"node_*"}})
	want = `a/
  bin
  z.go
    package z
empty/
.env
  SECRET=1
b.txt
  hello
big
`
	if got != want {
		t.Errorf("options:\ngot\n%s\nwant\n%s", got, want)
	}
}

func TestBadPattern(t *testing.T) {
	if _, err := Dir(t.TempDir(), Options{Ignore: []string{"["}}); err == nil {
		t.Error("want error for bad pattern")
	}
}

func TestGitignore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := fixture(t)
	if err := os.RemoveAll(filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, ".gitignore"), "node_modules/\n*.go\n")
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	got := format(t, root, Options{Gitignore: true})
	want := `a/
  bin
empty/
b.txt
big
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	if _, err := Dir(t.TempDir(), Options{Gitignore: true}); err == nil {
		t.Error("want error outside a git repository")
	}
}
