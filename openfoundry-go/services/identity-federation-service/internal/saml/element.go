package saml

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// element is the minimal namespaced XML tree the SAML validators
// walk. Mirrors the subset of `roxmltree::Node` operations the Rust
// impl uses: descendants(), children(), attribute(), tag_name()
// (namespace + local name), node text. Built by parseTree from an
// `encoding/xml` token stream — small enough to live alongside the
// validators rather than pulling a third-party tree library.
type element struct {
	Name     xml.Name
	Attrs    []xml.Attr
	Children []*element
	// Text is the concatenated CharData of this element's *direct*
	// children (matching roxmltree's `node.text()` semantics for
	// shallow text). Use elementText for the recursive variant.
	Text string
	// Parent kept so descendants() callers don't have to thread it
	// manually — left unset on the root.
	Parent *element
}

// parseTree builds a single root element from an XML byte stream.
// Strict mode is off (matching the Rust roxmltree default which
// tolerates xmlns redeclarations and comments). Returns ErrEmptyXML
// if there's no root element.
func parseTree(s string) (*element, error) {
	dec := xml.NewDecoder(strings.NewReader(s))
	dec.Strict = false

	var (
		root  *element
		stack []*element
	)
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			el := &element{Name: t.Name, Attrs: t.Attr}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				el.Parent = parent
				parent.Children = append(parent.Children, el)
			} else {
				root = el
			}
			stack = append(stack, el)
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	if root == nil {
		return nil, errors.New("xml: no root element")
	}
	return root, nil
}

// matches reports whether the element has the given namespace +
// local name. Mirrors fn `is_element_named`. An empty `ns` arg
// matches any namespace (used for parsers like metadata that walk
// IdP-published documents whose default namespaces vary).
func (e *element) matches(ns, local string) bool {
	if e == nil {
		return false
	}
	if e.Name.Local != local {
		return false
	}
	if ns == "" {
		return true
	}
	return e.Name.Space == ns
}

// attribute returns the named attribute's trimmed value if present
// and non-whitespace; nil otherwise. Mirrors the
// `node.attribute(name).and_then(trimmed)` pattern.
func (e *element) attribute(name string) *string {
	if e == nil {
		return nil
	}
	for _, a := range e.Attrs {
		if a.Name.Local == name {
			return trimmed(a.Value)
		}
	}
	return nil
}

// rawAttribute returns the attribute value verbatim (no trim, no
// nil-on-empty). Used by the InResponseTo / Recipient checks where
// we want to surface "present but empty" as a distinct error.
func (e *element) rawAttribute(name string) (string, bool) {
	if e == nil {
		return "", false
	}
	for _, a := range e.Attrs {
		if a.Name.Local == name {
			return a.Value, true
		}
	}
	return "", false
}

// findChild returns the first direct child matching (ns, local) or
// nil if none. Mirrors `children().find(is_element_named(ns, local))`.
func (e *element) findChild(ns, local string) *element {
	if e == nil {
		return nil
	}
	for _, c := range e.Children {
		if c.matches(ns, local) {
			return c
		}
	}
	return nil
}

// findDescendant returns the first descendant (DFS) matching
// (ns, local). Mirrors `descendants().find(is_element_named)`.
// Includes self.
func (e *element) findDescendant(ns, local string) *element {
	if e == nil {
		return nil
	}
	if e.matches(ns, local) {
		return e
	}
	for _, c := range e.Children {
		if got := c.findDescendant(ns, local); got != nil {
			return got
		}
	}
	return nil
}

// findDescendants returns every (ns, local)-matching descendant in
// document order, includes self. Mirrors
// `descendants().filter(is_element_named).collect()`.
func (e *element) findDescendants(ns, local string) []*element {
	if e == nil {
		return nil
	}
	var out []*element
	if e.matches(ns, local) {
		out = append(out, e)
	}
	for _, c := range e.Children {
		out = append(out, c.findDescendants(ns, local)...)
	}
	return out
}

// elementText concatenates every CharData in the element's subtree
// and returns the trimmed result, or "" if empty. Mirrors fn
// `node_text`.
func elementText(e *element) string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	collectText(e, &b)
	return strings.TrimSpace(b.String())
}

func collectText(e *element, b *strings.Builder) {
	if e == nil {
		return
	}
	b.WriteString(e.Text)
	for _, c := range e.Children {
		collectText(c, b)
	}
}
