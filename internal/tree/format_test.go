package tree

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustNodes(t *testing.T, s string) []*Node {
	t.Helper()
	var nodes []*Node
	if err := json.Unmarshal([]byte(s), &nodes); err != nil {
		t.Fatal(err)
	}
	return nodes
}

func TestFormat(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "canonical layout",
			src: `
project/
    backend/       # example backend
        store/
    docs/
        architecture.md
            # Architecture

              indented


    Makefile
`,
			want: `project/
  backend/  # example backend
    store/
  docs/
    architecture.md
      # Architecture

        indented
  Makefile
`,
		},
		{
			name: "shorthand is expanded",
			src:  "src/cmd/main.go  # entry\nsrc/internal/\n",
			want: "src/\n  cmd/\n    main.go  # entry\n  internal/\n",
		},
		{
			name: "tree(1) input is normalized",
			src:  "a/\n├── b/\n│   └── c\n└── d\n",
			want: "a/\n  b/\n    c\n  d\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes, err := Parse([]byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			got, err := Format(nodes)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestFormatQuoting(t *testing.T) {
	nodes := mustNodes(t, `[
		{"name":"#hash","type":"file"},
		{"name":" lead","type":"dir","children":[{"name":"x","type":"file"}]},
		{"name":"trail ","type":"file","comment":"c"},
		{"name":"a #b","type":"file"},
		{"name":"\"q\\","type":"file"},
		{"name":"├box","type":"file"},
		{"name":"plain name #ok","type":"file"},
		{"name":"a#b","type":"file"}
	]`)
	got, err := Format(nodes)
	if err != nil {
		t.Fatal(err)
	}
	want := `"#hash"
" lead/"
  x
"trail "  # c
"a #b"
"\"q\\"
"├box"
"plain name #ok"
a#b
`
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	back, err := Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	want2, _ := json.Marshal(nodes)
	assertJSON(t, back, string(want2))
}

func TestFormatErrors(t *testing.T) {
	tests := []struct {
		json string
		msg  string
	}{
		{`[{"name":"a/b","type":"file"}]`, "invalid name"},
		{`[{"name":"..","type":"dir"}]`, "invalid name"},
		{`[{"name":"a\nb","type":"file"}]`, "invalid name"},
		{`[{"name":"","type":"file"}]`, "invalid name"},
		{`[{"name":"a","type":"file","comment":"x\ny"}]`, "comment"},
		{`[{"name":"a","type":"file"},{"name":"a","type":"dir"}]`, "declared twice"},
		{`[{"name":"a","type":"dir","content":"x"}]`, "cannot have content"},
		{`[{"name":"a","type":"file","children":[{"name":"b","type":"file"}]}]`, "cannot have children"},
		{`[{"name":"a","type":"link"}]`, "unknown type"},
		{`[{"name":"d","type":"dir","children":[{"name":"..","type":"file"}]}]`, `"d/.."`},
		{`[{"name":"a","type":"file","content":"\n  x\n"}]`, "starts with indentation"},
	}
	for _, tt := range tests {
		_, err := Format(mustNodes(t, tt.json))
		if err == nil || !strings.Contains(err.Error(), tt.msg) {
			t.Errorf("%s: got %v, want error containing %q", tt.json, err, tt.msg)
		}
	}

	// once the unit is fixed by an earlier line, indented content is fine
	nodes := mustNodes(t, `[{"name":"d","type":"dir","children":[{"name":"x","type":"file"}]},
		{"name":"a","type":"file","content":"  x\n"}]`)
	if _, err := Format(nodes); err != nil {
		t.Error(err)
	}
}

// Parse → Format → Parse keeps the AST, and Format output is a fixed point.
func TestRoundTrip(t *testing.T) {
	for _, tt := range parseTests {
		t.Run(tt.name, func(t *testing.T) {
			nodes, err := Parse([]byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			out, err := Format(nodes)
			if err != nil {
				t.Fatal(err)
			}
			back, err := Parse(out)
			if err != nil {
				t.Fatalf("reparse: %v\n%s", err, out)
			}
			assertJSON(t, back, tt.want)
			again, _ := Format(back)
			if string(again) != string(out) {
				t.Errorf("not a fixed point:\n%s\nvs\n%s", out, again)
			}
		})
	}
}
