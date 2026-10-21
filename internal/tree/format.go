package tree

import (
	"bytes"
	"fmt"
	"strings"
)

const unit = "  "

// Format renders nodes as a canonical .tree document with a 2-space indent.
// Content is clipped the same way Parse reads it, so use JSON or YAML when exact bytes matter.
func Format(nodes []*Node) ([]byte, error) {
	if err := Validate(nodes); err != nil {
		return nil, err
	}
	f := &formatter{}
	if err := f.nodes(nodes, 0, ""); err != nil {
		return nil, err
	}
	return f.buf.Bytes(), nil
}

type formatter struct {
	buf bytes.Buffer
	// whether an indented line has been written, which fixes the parser's indent unit
	indented bool
}

func (f *formatter) nodes(nodes []*Node, depth int, dir string) error {
	pad := strings.Repeat(unit, depth)
	for _, n := range nodes {
		name := n.Name
		if n.Type == Dir {
			name += "/"
		}
		if needsQuote(n.Name) {
			name = quote(name)
		}
		f.buf.WriteString(pad + name)
		if n.Comment != "" {
			f.buf.WriteString("  # " + n.Comment)
		}
		f.buf.WriteByte('\n')
		if depth > 0 {
			f.indented = true
		}

		var err error
		switch {
		case n.Type == Dir:
			err = f.nodes(n.Children, depth+1, dir+n.Name+"/")
		case n.Content != nil:
			err = f.body(*n.Content, pad+unit, dir+n.Name)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (f *formatter) body(content, prefix, path string) error {
	lines := strings.Split(content, "\n")
	for len(lines) > 0 && isBlank([]rune(lines[len(lines)-1]), false) {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}

	if !f.indented {
		// this body is the first indented line, so its leading indentation would change the unit
		for _, l := range lines {
			if r := []rune(l); !isBlank(r, false) {
				if indentWidth(r) > 0 {
					return fmt.Errorf("%q: content starts with indentation, which the tree format cannot express here; use json or yaml", path)
				}
				break
			}
		}
		f.indented = true
	}

	for _, l := range lines {
		if isBlank([]rune(l), false) {
			f.buf.WriteByte('\n')
			continue
		}
		f.buf.WriteString(prefix + l + "\n")
	}
	return nil
}

// Expressible reports whether content survives Format and Parse unchanged wherever its file appears.
func Expressible(content string) bool {
	out, err := Format([]*Node{{Name: "f", Type: File, Content: &content}})
	if err != nil {
		return false
	}
	back, err := Parse(out)
	return err == nil && back[0].Content != nil && *back[0].Content == content
}

// needsQuote reports whether Parse would read name differently without quotes.
func needsQuote(name string) bool {
	first := []rune(name)[0]
	last := name[len(name)-1]
	return first == '"' || first == '#' || first == '\t' || isIndent(first) ||
		last == ' ' || last == '\t' ||
		strings.Contains(name, " #") || strings.Contains(name, "\t#")
}

func quote(name string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(name) + `"`
}
