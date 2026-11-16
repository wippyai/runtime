// SPDX-License-Identifier: MPL-2.0

package xml

import "strings"

// Text concatenates all descendant character data.
func Text(e *Element) string {
	var b strings.Builder
	var walk func(*Element)
	walk = func(e *Element) {
		if e == nil {
			return
		}
		b.WriteString(e.Text)
		for _, v := range e.Children {
			switch c := v.(type) {
			case string:
				b.WriteString(c)
			case *Element:
				walk(c)
			}
		}
	}
	walk(e)
	return b.String()
}

// Attr looks up an attribute by expanded name.
func Attr(e *Element, name, ns string) (string, bool) {
	if e == nil {
		return "", false
	}
	v, ok := e.Attrs[AttrKey(ns, name)]
	return v, ok
}

// Child returns the first matching child element.
func Child(e *Element, name string, ns *string) *Element {
	if e != nil {
		for _, v := range e.Children {
			if c, ok := v.(*Element); ok && c != nil && c.Name == name && (ns == nil || c.NS == *ns) {
				return c
			}
		}
	}
	return nil
}

// Children returns child elements matching optional name and namespace filters.
func Children(e *Element, name, ns *string) []*Element {
	var out []*Element
	if e != nil {
		for _, v := range e.Children {
			if c, ok := v.(*Element); ok && c != nil && (name == nil || c.Name == *name) && (ns == nil || c.NS == *ns) {
				out = append(out, c)
			}
		}
	}
	return out
}

// Resolve resolves a QName using the element namespace bindings.
func Resolve(e *Element, name string) (string, string, error) {
	if e == nil {
		return "", "", problem("argument", "element is required")
	}
	return ResolveQName(name, func(prefix string) (string, bool) {
		uri, ok := e.XMLNS[prefix]
		return uri, ok
	})
}

// ResolveQName splits a QName and resolves its prefix through lookup. An unprefixed
// name takes the default namespace when lookup binds one.
func ResolveQName(name string, lookup func(prefix string) (string, bool)) (string, string, error) {
	p, n, err := qname(name)
	if err != nil {
		return "", "", err
	}
	if p == "xml" {
		return n, XMLNamespace, nil
	}
	ns, ok := lookup(p)
	if !ok && p != "" {
		return "", "", problem("namespace", "unbound QName prefix")
	}
	return n, ns, nil
}
