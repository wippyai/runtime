// SPDX-License-Identifier: MPL-2.0

package xml

import (
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/system/xml"
)

// treeWriter serializes Element tables, enforcing the normal form. A violation is
// recorded on the encoder as a terminal element error, as for any other invalid input.
type treeWriter struct {
	w     *xml.Encoder
	attrs []xml.AttrValue
	decls []xml.Binding
}

func (e *treeWriter) reject(msg string) error { return e.w.Fail("element", msg) }

// elementFields holds the fields of an Element table; absent fields are nil.
type elementFields struct {
	name, ns, text, attrs, xmlns, children lua.LValue
}

// fieldsOf reads the Element fields in one pass over the table's string keys; other
// keys are ignored.
func fieldsOf(t *lua.LTable) elementFields {
	var f elementFields
	for k, v := range t.Strdict {
		switch k {
		case "name":
			f.name = v
		case "ns":
			f.ns = v
		case "text":
			f.text = v
		case "attrs":
			f.attrs = v
		case "xmlns":
			f.xmlns = v
		case "children":
			f.children = v
		}
	}
	return f
}

func absent(v lua.LValue) bool { return v == nil || v == lua.LNil }

// stringField reads an optional string field; a present field must be a non-empty string.
func (e *treeWriter) stringField(v lua.LValue, key string) (string, bool, error) {
	if absent(v) {
		return "", false, nil
	}
	switch v := v.(type) {
	case lua.LString:
		if v == "" && key != "ns" {
			return "", false, e.reject(key + " must not be empty")
		}
		return string(v), true, nil
	default:
		return "", false, e.reject(key + " must be a string")
	}
}

// stringTable reads an optional non-empty table of string keys and string values.
func (e *treeWriter) stringTable(v lua.LValue, key string, visit func(k, v string) error) error {
	if absent(v) {
		return nil
	}
	m, ok := v.(*lua.LTable)
	if !ok {
		return e.reject(key + " must be a table")
	}
	if len(m.Dict) > 0 {
		for _, x := range m.Dict {
			if x != lua.LNil {
				return e.reject(key + " keys must be strings")
			}
		}
	}
	for _, x := range m.Array {
		if x != lua.LNil {
			return e.reject(key + " keys must be strings")
		}
	}
	n := 0
	for k, x := range m.Strdict {
		if x == lua.LNil {
			continue
		}
		s, ok := x.(lua.LString)
		if !ok {
			return e.reject(key + " values must be strings")
		}
		if err := visit(k, string(s)); err != nil {
			return err
		}
		n++
	}
	if n == 0 {
		return e.reject("empty " + key + " must be omitted")
	}
	return nil
}

// content returns the dense children array and whether it holds text.
func (e *treeWriter) content(v lua.LValue) ([]lua.LValue, bool, error) {
	if absent(v) {
		return nil, false, nil
	}
	c, ok := v.(*lua.LTable)
	if !ok {
		return nil, false, e.reject("children must be an array")
	}
	for _, x := range c.Strdict {
		if x != lua.LNil {
			return nil, false, e.reject("children must be an array")
		}
	}
	for _, x := range c.Dict {
		if x != lua.LNil {
			return nil, false, e.reject("children must be a dense array")
		}
	}
	items := c.Array[:c.Len()]
	if len(items) == 0 {
		return nil, false, e.reject("empty children must be omitted")
	}
	hasElement, hasText, lastText := false, false, false
	for _, x := range items {
		switch s := x.(type) {
		case lua.LString:
			if s == "" || lastText {
				return nil, false, e.reject("children text must be non-empty and not adjacent")
			}
			hasText, lastText = true, true
		case *lua.LTable:
			hasElement, lastText = true, false
		default:
			return nil, false, e.reject("children must hold strings and elements")
		}
	}
	if !hasElement {
		return nil, false, e.reject("text-only content must use text")
	}
	return items, hasText, nil
}

func (e *treeWriter) element(t *lua.LTable) error {
	f := fieldsOf(t)
	name, ok := f.name.(lua.LString)
	if !ok {
		return e.reject("element name must be a string")
	}
	ns, _, err := e.stringField(f.ns, "ns")
	if err != nil {
		return err
	}
	text, hasText, err := e.stringField(f.text, "text")
	if err != nil {
		return err
	}
	children, mixed, err := e.content(f.children)
	if err != nil {
		return err
	}
	if hasText && children != nil {
		return e.reject("an element has text or children, never both")
	}
	attrs := e.attrs[:0]
	err = e.stringTable(f.attrs, "attrs", func(k, v string) error {
		attrs = append(attrs, xml.AttrValue{Key: k, Value: v})
		return nil
	})
	if err != nil {
		return err
	}
	e.attrs = attrs
	decls := e.decls[:0]
	err = e.stringTable(f.xmlns, "xmlns", func(k, v string) error {
		if k == "xml" {
			return e.w.Fail("namespace", "the xml binding is implicit")
		}
		decls = append(decls, xml.Binding{Prefix: k, URI: v})
		return nil
	})
	if err != nil {
		return err
	}
	e.decls = decls
	if err = e.w.StartNode(string(name), ns, attrs, decls, mixed || hasText); err != nil {
		return err
	}
	if hasText {
		if err = e.w.Text(text); err != nil {
			return err
		}
	}
	for _, c := range children {
		switch v := c.(type) {
		case lua.LString:
			err = e.w.Text(string(v))
		case *lua.LTable:
			err = e.element(v)
		}
		if err != nil {
			return err
		}
	}
	return e.w.EndElement()
}

// writeTree serializes an Element table into w.
func writeTree(w *xml.Encoder, t *lua.LTable) error {
	e := treeWriter{w: w}
	return e.element(t)
}
