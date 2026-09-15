package parser

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Limits protect the worker from hostile or broken feeds (§154, §192).
const (
	maxDepth     = 64
	maxNodes     = 60000
	maxTextBytes = 6 << 20
)

// node is a lightweight XML element used instead of struct mapping so that one parser
// can serve RSS 2.0, RSS 1.0/RDF, Atom and their namespace extensions (§155).
type node struct {
	Name     xml.Name
	Attrs    []xml.Attr
	Text     string
	Children []*node
}

// parseTree decodes XML into a bounded tree. A mid-document syntax error does not
// discard what was already parsed: recoverable feeds keep their items (§155, §53).
func parseTree(r io.Reader) (*node, error) {
	dec := xml.NewDecoder(r)
	// Lenient tokenization: real feeds contain undeclared entities, stray markup and
	// unescaped ampersands. AutoClose is deliberately NOT enabled because it treats
	// elements such as <link> as void HTML tags, which silently destroys RSS documents.
	dec.Strict = false
	dec.Entity = xml.HTMLEntity

	root := &node{Name: xml.Name{Local: "#document"}}
	stack := []*node{root}
	nodes := 0
	textBytes := 0
	var firstErr error

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			nodes++
			if nodes > maxNodes {
				return root, fmt.Errorf("xml: node limit exceeded")
			}
			if len(stack) > maxDepth {
				return root, fmt.Errorf("xml: depth limit exceeded")
			}
			n := &node{Name: t.Name, Attrs: t.Attr}
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, n)
			stack = append(stack, n)
		case xml.CharData:
			textBytes += len(t)
			if textBytes > maxTextBytes {
				return root, fmt.Errorf("xml: text limit exceeded")
			}
			stack[len(stack)-1].Text += string(t)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if len(root.Children) == 0 {
		if firstErr != nil {
			return root, firstErr
		}
		return root, fmt.Errorf("xml: document is empty")
	}
	return root, nil
}

// documentRoot returns the single document element, or an empty node.
func documentRoot(root *node) *node {
	for _, c := range root.Children {
		if strings.HasPrefix(c.Name.Local, "#") {
			continue
		}
		return c
	}
	return &node{}
}

// local returns the first child whose local name matches, case-insensitively.
func child(n *node, names ...string) *node {
	for _, c := range n.Children {
		for _, want := range names {
			if strings.EqualFold(c.Name.Local, want) {
				return c
			}
		}
	}
	return nil
}

// children returns every child whose local name matches.
func childrenMatches(n *node, names ...string) []*node {
	out := []*node{}
	for _, c := range n.Children {
		for _, want := range names {
			if strings.EqualFold(c.Name.Local, want) {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// descendants returns every descendant whose local name matches (namespace-insensitive).
func descendants(n *node, names ...string) []*node {
	out := []*node{}
	var walk func(cur *node)
	walk = func(cur *node) {
		for _, c := range cur.Children {
			for _, want := range names {
				if strings.EqualFold(c.Name.Local, want) {
					out = append(out, c)
					break
				}
			}
			walk(c)
		}
	}
	walk(n)
	return out
}

// attr returns an attribute value by local name, ignoring the namespace prefix.
func attr(n *node, names ...string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attrs {
		for _, want := range names {
			if strings.EqualFold(a.Name.Local, want) {
				return strings.TrimSpace(a.Value)
			}
		}
	}
	return ""
}

// innerText returns the concatenated character data of a node and its children.
func innerText(n *node) string {
	if n == nil {
		return ""
	}
	var sb strings.Builder
	var walk func(cur *node)
	walk = func(cur *node) {
		sb.WriteString(cur.Text)
		for _, c := range cur.Children {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(sb.String())
}

// innerHTML re-serializes a node's children so that feed-provided markup (links,
// emphasis) survives into the article content before sanitization.
func innerHTML(n *node) string {
	if n == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range n.Children {
		serialize(&sb, c)
	}
	if sb.Len() == 0 {
		return strings.TrimSpace(n.Text)
	}
	return strings.TrimSpace(sb.String())
}

// rawText returns raw character data without descending into children (used for
// content elements that wrap their payload in CDATA).
func rawText(n *node) string {
	if n == nil {
		return ""
	}
	if len(n.Children) == 0 {
		return strings.TrimSpace(n.Text)
	}
	return innerHTML(n)
}

var voidElements = map[string]bool{
	"br": true, "img": true, "hr": true, "meta": true, "link": true, "input": true,
}

func serialize(sb *strings.Builder, n *node) {
	name := n.Name.Local
	sb.WriteString("<")
	sb.WriteString(name)
	for _, a := range n.Attrs {
		an := a.Name.Local
		if an == "" || strings.HasPrefix(an, "xmlns") {
			continue
		}
		sb.WriteString(" ")
		sb.WriteString(an)
		sb.WriteString(`="`)
		sb.WriteString(escapeAttr(a.Value))
		sb.WriteString(`"`)
	}
	sb.WriteString(">")
	if voidElements[strings.ToLower(name)] {
		return
	}
	sb.WriteString(n.Text)
	for _, c := range n.Children {
		serialize(sb, c)
	}
	sb.WriteString("</")
	sb.WriteString(name)
	sb.WriteString(">")
}

func escapeAttr(s string) string {
	return strings.NewReplacer(`&`, "&amp;", `"`, "&quot;", `<`, "&lt;", `>`, "&gt;").Replace(s)
}
