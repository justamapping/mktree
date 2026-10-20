package tree

import (
	"fmt"
	"strings"
)

type Type string

const (
	Dir  Type = "dir"
	File Type = "file"
)

type Node struct {
	Name     string  `json:"name"`
	Type     Type    `json:"type"`
	Comment  string  `json:"comment,omitempty"`
	Children []*Node `json:"children,omitempty"`
	// nil means "ensure exists", "" means "ensure empty".
	Content *string `json:"content,omitempty"`
	Line    int     `json:"-"`
}

// Validate checks that nodes form a tree that can be written to disk.
func Validate(nodes []*Node) error {
	return validate(nodes, "")
}

func validate(nodes []*Node, dir string) error {
	seen := make(map[string]bool)
	for _, n := range nodes {
		p := dir + n.Name
		switch {
		case n.Name == "", n.Name == ".", n.Name == "..", strings.ContainsAny(n.Name, "/\x00\n\r"):
			return fmt.Errorf("%q: invalid name", p)
		case strings.ContainsAny(n.Comment, "\n\r"):
			return fmt.Errorf("%q: comment cannot span lines", p)
		case seen[n.Name]:
			return fmt.Errorf("%q: declared twice", p)
		}
		seen[n.Name] = true

		switch n.Type {
		case Dir:
			if n.Content != nil {
				return fmt.Errorf("%q: directories cannot have content", p)
			}
			if err := validate(n.Children, p+"/"); err != nil {
				return err
			}
		case File:
			if len(n.Children) > 0 {
				return fmt.Errorf("%q: files cannot have children", p)
			}
		default:
			return fmt.Errorf("%q: unknown type %q", p, n.Type)
		}
	}
	return nil
}

func (n *Node) child(name string) *Node {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}
