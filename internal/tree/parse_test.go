package tree

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

var parseTests = []struct {
	name string
	src  string
	want string
}{
	{
		name: "dirs, files, comments, content",
		src: `
project/
  backend/                  # example backend
    internal/
      store/                # the db
  docs/
    architecture.md
      hello world!
  Makefile
`,
		want: `[{"name":"project","type":"dir","children":[
				{"name":"backend","type":"dir","comment":"example backend","children":[
					{"name":"internal","type":"dir","children":[{"name":"store","type":"dir","comment":"the db"}]}]},
				{"name":"docs","type":"dir","children":[{"name":"architecture.md","type":"file","content":"hello world!\n"}]},
				{"name":"Makefile","type":"file"}]}]`,
	},
	{
		name: "body is raw after the prefix",
		src: "Makefile\n" +
			"  # build\n" +
			"  build:\n" +
			"  \tgo build ./...\n" +
			"\n" +
			"    indented\n" +
			"\n" +
			"        \n" +
			"README.md\n",
		want: `[{"name":"Makefile","type":"file","content":"# build\nbuild:\n\tgo build ./...\n\n  indented\n"},
				{"name":"README.md","type":"file"}]`,
	},
	{
		name: "body sets the unit when it is the first indented line",
		src: `
README.md
    hello
      world
src/
    main.go
`,
		want: `[{"name":"README.md","type":"file","content":"hello\n  world\n"},
				{"name":"src","type":"dir","children":[{"name":"main.go","type":"file"}]}]`,
	},
	{
		name: "body keeps tree diagrams",
		src: `
docs/
  README.md
    src/
    ├── a/
    │   └── b.go
    │
    └── c.go
  x
`,
		want: `[{"name":"docs","type":"dir","children":[
				{"name":"README.md","type":"file","content":"src/\n├── a/\n│   └── b.go\n│\n└── c.go\n"},
				{"name":"x","type":"file"}]}]`,
	},
	{
		name: "shorthand paths merge into existing dirs",
		src: `
src/cmd/main.go
src/internal/
  tree.go
src/cmd/  # entry
`,
		want: `[{"name":"src","type":"dir","children":[
				{"name":"cmd","type":"dir","comment":"entry","children":[{"name":"main.go","type":"file"}]},
				{"name":"internal","type":"dir","children":[{"name":"tree.go","type":"file"}]}]}]`,
	},
	{
		name: "comments and quoted names",
		src: `
# header
a#b.txt
c.txt   # note
"weird # name"  # quoted
"  padded \"q\" "
"my dir/"
  x
`,
		want: `[{"name":"a#b.txt","type":"file"},
				{"name":"c.txt","type":"file","comment":"note"},
				{"name":"weird # name","type":"file","comment":"quoted"},
				{"name":"  padded \"q\" ","type":"file"},
				{"name":"my dir","type":"dir","children":[{"name":"x","type":"file"}]}]`,
	},
	{
		name: "tree(1) style input",
		src: `
project/
├── backend/
│   └── store/
│       └── db.go
│
└── Makefile
`,
		want: `[{"name":"project","type":"dir","children":[
				{"name":"backend","type":"dir","children":[
					{"name":"store","type":"dir","children":[{"name":"db.go","type":"file"}]}]},
				{"name":"Makefile","type":"file"}]}]`,
	},
	{
		name: "tree(1) with non-breaking spaces",
		src:  "a/\n├── b/\n│   └── c\n└── d\n",
		want: `[{"name":"a","type":"dir","children":[
				{"name":"b","type":"dir","children":[{"name":"c","type":"file"}]},
				{"name":"d","type":"file"}]}]`,
	},
	{
		name: "CRLF line endings",
		src:  "a/\r\n  b.txt\r\n    hi\r\n",
		want: `[{"name":"a","type":"dir","children":[{"name":"b.txt","type":"file","content":"hi\n"}]}]`,
	},
	{
		name: "empty document",
		src:  "\n# nothing\n\n",
		want: `null`,
	},
}

func TestParse(t *testing.T) {
	for _, tt := range parseTests {
		t.Run(tt.name, func(t *testing.T) {
			nodes, err := Parse([]byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			assertJSON(t, nodes, tt.want)
		})
	}
}

func assertJSON(t *testing.T, nodes []*Node, want string) {
	t.Helper()
	got, _ := json.Marshal(nodes)
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(want)); err != nil {
		t.Fatalf("bad want: %v", err)
	}
	if string(got) != compact.String() {
		t.Errorf("got  %s\nwant %s", got, compact.String())
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		src  string
		line int
		msg  string
	}{
		{"a/\n\tb", 2, "tabs"},
		{"a/\n  b/\n   c", 3, "not a multiple of 2"},
		{"a/\n  b/\n      c", 3, "unexpected indentation"},
		{"  a", 1, "unexpected indentation"},
		{"../x", 1, `".." segments`},
		{"a/./b", 1, `"." segments`},
		{"/etc/passwd", 1, "absolute"},
		{"a//b", 1, "empty path segment"},
		{`""`, 1, "empty name"},
		{"a.md\n    x\n  y", 3, "indented 4 columns"},
		{"a/\n  b.md\n    x\n   \ty", 4, "tabs"},
		{"a\na", 2, "already declared on line 1"},
		{"a\na/b", 2, "already declared on line 1"},
		{"a/\na", 2, "already declared on line 1"},
		{`"abc`, 1, "unterminated"},
		{`"a" b`, 1, "unexpected text"},
		{`"a\n"`, 1, "invalid escape"},
	}

	for _, tt := range tests {
		_, err := Parse([]byte(tt.src))
		var perr *Error
		if !errors.As(err, &perr) {
			t.Errorf("%q: got %v, want parse error", tt.src, err)
			continue
		}
		if perr.Line != tt.line || !strings.Contains(perr.Msg, tt.msg) {
			t.Errorf("%q: got %v, want line %d containing %q", tt.src, perr, tt.line, tt.msg)
		}
	}
}
