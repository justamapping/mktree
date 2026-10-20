package tree

import (
	"errors"
	"fmt"
	"strings"
)

type Error struct {
	Line, Col int
	Msg       string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Msg)
}

type parser struct {
	lines []string
	unit  int
	root  *Node
	// stack[d] is the directory that receives children at depth d.
	stack []*Node
}

// Parse reads a .tree document into a list of top-level nodes.
func Parse(src []byte) ([]*Node, error) {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	p := &parser{lines: strings.Split(text, "\n"), root: &Node{Type: Dir}}
	p.stack = []*Node{p.root}
	for i := 0; i < len(p.lines); {
		next, err := p.parseLine(i)
		if err != nil {
			return nil, err
		}
		i = next
	}
	return p.root.Children, nil
}

func (p *parser) parseLine(i int) (int, error) {
	runes := []rune(p.lines[i])
	if isBlank(runes, true) {
		return i + 1, nil
	}
	w := indentWidth(runes)
	rest := strings.TrimRight(string(runes[w:]), " \t")
	errorf := func(format string, args ...any) error {
		return &Error{Line: i + 1, Col: w + 1, Msg: fmt.Sprintf(format, args...)}
	}
	switch {
	case rest == "", rest[0] == '#':
		return i + 1, nil
	case rest[0] == '\t':
		return 0, errorf("tabs are not allowed in indentation")
	}

	if w > 0 && p.unit == 0 {
		p.unit = w
	}
	depth := 0
	if p.unit > 0 {
		if w%p.unit != 0 {
			return 0, errorf("indentation of %d is not a multiple of %d", w, p.unit)
		}
		depth = w / p.unit
	}
	if depth >= len(p.stack) {
		return 0, errorf("unexpected indentation")
	}
	p.stack = p.stack[:depth+1]

	name, comment, err := splitName(rest)
	if err != nil {
		return 0, errorf("%v", err)
	}
	segs, isDir, err := splitPath(name)
	if err != nil {
		return 0, errorf("%v", err)
	}
	node, err := add(p.stack[depth], segs, isDir, comment, i+1)
	if err != nil {
		return 0, errorf("%v", err)
	}
	if isDir {
		p.stack = append(p.stack, node)
		return i + 1, nil
	}
	return p.parseBody(node, i, w)
}

// parseBody consumes the lines indented under the file on line i.
func (p *parser) parseBody(node *Node, i, w int) (int, error) {
	if p.unit == 0 {
		// the body may hold the first indented line in the document
		for j := i + 1; j < len(p.lines); j++ {
			if r := []rune(p.lines[j]); !isBlank(r, false) {
				p.unit = indentWidth(r)
				break
			}
		}
		if p.unit == 0 {
			return i + 1, nil
		}
	}

	prefix := w + p.unit
	var body []string
	j := i + 1
	for ; j < len(p.lines); j++ {
		r := []rune(p.lines[j])
		if isBlank(r, false) {
			body = append(body, "")
			continue
		}
		n := indentWidth(r)
		if n <= w {
			break
		}
		if n < prefix {
			msg := fmt.Sprintf("file content must be indented %d columns", prefix)
			if r[n] == '\t' {
				msg = "tabs are not allowed in indentation"
			}
			return 0, &Error{Line: j + 1, Col: n + 1, Msg: msg}
		}
		body = append(body, string(r[prefix:]))
	}

	for len(body) > 0 && body[len(body)-1] == "" {
		body = body[:len(body)-1]
	}
	if len(body) > 0 {
		s := strings.Join(body, "\n") + "\n"
		node.Content = &s
	}
	return j, nil
}

// add creates the path segs under parent, merging into existing directories.
func add(parent *Node, segs []string, isDir bool, comment string, line int) (*Node, error) {
	for k, name := range segs {
		typ := Dir
		if k == len(segs)-1 && !isDir {
			typ = File
		}
		node := parent.child(name)
		switch {
		case node == nil:
			node = &Node{Name: name, Type: typ, Line: line}
			parent.Children = append(parent.Children, node)
		case node.Type == File || typ == File:
			return nil, fmt.Errorf("%q already declared on line %d", name, node.Line)
		}
		parent = node
	}
	if parent.Comment == "" {
		parent.Comment = comment
	}
	return parent, nil
}

// splitName separates a tree line into its name and trailing comment.
func splitName(s string) (name, comment string, err error) {
	if !strings.HasPrefix(s, `"`) {
		for i := 1; i < len(s); i++ {
			if s[i] == '#' && (s[i-1] == ' ' || s[i-1] == '\t') {
				return strings.TrimRight(s[:i], " \t"), strings.TrimSpace(s[i+1:]), nil
			}
		}
		return s, "", nil
	}

	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			if i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\') {
				i++
				b.WriteByte(s[i])
				continue
			}
			return "", "", errors.New(`invalid escape in quoted name, only \" and \\ are allowed`)
		case '"':
			after := strings.TrimLeft(s[i+1:], " \t")
			switch {
			case after == "":
				return b.String(), "", nil
			case after[0] == '#':
				return b.String(), strings.TrimSpace(after[1:]), nil
			}
			return "", "", errors.New("unexpected text after quoted name")
		default:
			b.WriteByte(c)
		}
	}
	return "", "", errors.New("unterminated quoted name")
}

func splitPath(name string) (segs []string, isDir bool, err error) {
	switch {
	case name == "":
		return nil, false, errors.New("empty name")
	case strings.HasPrefix(name, "/"):
		return nil, false, errors.New("absolute paths are not allowed")
	}
	isDir = strings.HasSuffix(name, "/")
	segs = strings.Split(strings.TrimSuffix(name, "/"), "/")
	for _, s := range segs {
		switch {
		case s == "":
			return nil, false, fmt.Errorf("empty path segment in %q", name)
		case s == "." || s == "..":
			return nil, false, fmt.Errorf("%q segments are not allowed", s)
		case strings.ContainsRune(s, 0):
			return nil, false, errors.New("names cannot contain NUL")
		}
	}
	return segs, isDir, nil
}

// isIndent reports whether r counts as one column of indentation, so tree(1) output parses.
func isIndent(r rune) bool {
	switch r {
	case ' ', ' ', '│', '├', '└', '─':
		return true
	}
	return false
}

func indentWidth(r []rune) int {
	n := 0
	for n < len(r) && isIndent(r[n]) {
		n++
	}
	return n
}

// isBlank reports whether a line is whitespace only. Between tree lines, a lone │ spacer is also blank.
func isBlank(r []rune, treeLevel bool) bool {
	for _, c := range r {
		switch {
		case c == ' ', c == '\t', c == ' ':
		case c == '│' && treeLevel:
		default:
			return false
		}
	}
	return true
}
