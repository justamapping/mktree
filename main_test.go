package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mktree(t *testing.T, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut strings.Builder
	code = run(args, strings.NewReader(stdin), &out, &errOut)
	return out.String(), errOut.String(), code
}

func expect(t *testing.T, name, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s:\ngot\n%s\nwant\n%s", name, got, want)
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

const project = `project/
  docs/
    a.md
      hello
  Makefile
`

func TestApply(t *testing.T) {
	dir := t.TempDir()

	out, _, code := mktree(t, project, "-n", dir)
	expect(t, "dry run", out, "+ project/\n+ project/docs/\n+ project/docs/a.md\n+ project/Makefile\n")
	if code != 0 {
		t.Errorf("dry run exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "project")); err == nil {
		t.Fatal("dry run wrote to disk")
	}

	if _, errOut, code := mktree(t, project, "-q", dir); code != 0 || errOut != "" {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "project/docs/a.md"))
	expect(t, "a.md", string(b), "hello\n")

	out, _, _ = mktree(t, project, dir)
	expect(t, "rerun", out, "")
	out, _, _ = mktree(t, project, "-v", dir)
	expect(t, "verbose", out, "= project/\n= project/docs/\n= project/docs/a.md\n= project/Makefile\n")
}

func TestApplyConflict(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.md"), "old\n")
	src := "a.md\n  new\nb.md\n"

	out, errOut, code := mktree(t, src, "-q", dir)
	expect(t, "stdout", out, "! a.md   content differs\n")
	expect(t, "stderr", errOut, "mktree: 1 conflict, nothing written\n")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.md")); err == nil {
		t.Error("b.md written despite conflict")
	}

	out, _, code = mktree(t, src, "--force", dir)
	expect(t, "force", out, "~ a.md\n+ b.md\n")
	if code != 0 {
		t.Errorf("force exit %d", code)
	}
}

func TestErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.tree")
	write(t, bad, "a/\n\tb\n")

	tests := []struct {
		stdin  string
		args   []string
		stderr string
		code   int
	}{
		{"a/\n\tb\n", []string{dir}, "mktree: <stdin>:2:1: tabs are not allowed", 2},
		{"", []string{"-f", bad, dir}, "mktree: " + bad + ":2:1: tabs", 2},
		{`[{"name":"a","typ":"file"}]`, []string{"--from", "json", dir}, `mktree: <stdin>: json: unknown field "typ"`, 2},
		{"- name: ..\n  type: dir\n", []string{"--from", "yaml", dir}, `mktree: <stdin>: "..": invalid name`, 2},
		{"", []string{"--from", "xml"}, `mktree: unknown format "xml"`, 2},
		{"", []string{"--nope"}, "mktree: flag provided but not defined: -nope\nrun 'mktree -h'", 2},
		{"", []string{dir, "extra"}, `mktree: unexpected argument "extra"`, 2},
		{"", []string{"dump", "--to", "toml"}, `mktree: unknown format "toml"`, 2},
		{"a\n", []string{filepath.Join(dir, "missing")}, "mktree: stat ", 1},
	}
	for _, tt := range tests {
		_, errOut, code := mktree(t, tt.stdin, tt.args...)
		if !strings.HasPrefix(errOut, tt.stderr) || code != tt.code {
			t.Errorf("%v: got exit %d %q, want exit %d %q", tt.args, code, errOut, tt.code, tt.stderr)
		}
	}

	out, _, code := mktree(t, "", "dump", "-h")
	if code != 0 || !strings.HasPrefix(out, "usage:") {
		t.Errorf("-h: exit %d, %q", code, out)
	}
}

func TestConvert(t *testing.T) {
	src := "src/main.go  # entry\n  package main\n\n  func main() {}\n"
	want := "src/\n  main.go  # entry\n    package main\n\n    func main() {}\n"

	out := src
	for _, step := range [][]string{{"--to", "json"}, {"--from", "json", "--to", "yaml"}, {"--from", "yaml"}} {
		var errOut string
		var code int
		out, errOut, code = mktree(t, out, append([]string{"convert"}, step...)...)
		if code != 0 {
			t.Fatalf("%v: exit %d: %s", step, code, errOut)
		}
	}
	expect(t, "tree → json → yaml → tree", out, want)

	out, _, _ = mktree(t, "", "convert", "--to", "json")
	expect(t, "empty", out, "[]\n")
}

// dump DIR | mktree DIR reports no changes, in every output format
func TestDumpRoundTrip(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a/b.txt"), "hello\n")
	write(t, filepath.Join(dir, "a/no-newline"), "x")
	write(t, filepath.Join(dir, "Makefile"), "all:\n\techo <hi> & bye\n")

	out, errOut, _ := mktree(t, "", "dump", "--contents", dir)
	expect(t, "tree", out, "a/\n  b.txt\n    hello\n  no-newline\nMakefile\n  all:\n  \techo <hi> & bye\n")
	expect(t, "warning", errOut, "mktree: a/no-newline: content omitted, the tree format cannot express it exactly (use --to json)\n")

	for _, to := range []string{"tree", "json", "yaml"} {
		doc, _, _ := mktree(t, "", "dump", "--contents", "--to", to, dir)
		plan, errOut, code := mktree(t, doc, "-n", "-v", "--from", to, dir)
		if code != 0 || strings.ContainsAny(plan, "+~!") {
			t.Errorf("%s: exit %d %s\n%s", to, code, errOut, plan)
		}
	}
}

func TestUsageExample(t *testing.T) {
	dir := t.TempDir()
	if _, errOut, code := mktree(t, example, "-q", dir); code != 0 {
		t.Fatalf("example in usage does not apply: %s", errOut)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "project/src/main.go"))
	expect(t, "main.go", string(b), "package main\n\nfunc main() {}\n")
}

// code blocks in the howto are valid input
func TestHowto(t *testing.T) {
	out, _, code := mktree(t, "", "--howto")
	if code != 0 || out != howto {
		t.Fatalf("--howto: exit %d", code)
	}
	blocks := 0
	for _, part := range strings.Split(howto, "```")[1:] {
		lang, body, _ := strings.Cut(part, "\n")
		if lang != "tree" && lang != "json" {
			continue
		}
		blocks++
		if _, errOut, code := mktree(t, body, "--from", lang, "-q", t.TempDir()); code != 0 {
			t.Errorf("%s block does not apply: %s\n%s", lang, errOut, body)
		}
	}
	if blocks == 0 {
		t.Error("no tree or json blocks found")
	}
}
