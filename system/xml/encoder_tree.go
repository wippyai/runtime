// SPDX-License-Identifier: MPL-2.0

package xml

// Element writes a tree using its complete children to decide indentation. Its output
// is bounded by MaxNodeBytes.
func (w *Encoder) Element(e *Element) error {
	w.BeginTree()
	defer w.EndTree()
	return w.element(e, 0)
}

func (w *Encoder) element(e *Element, depth int) error {
	if e == nil || e.Text != "" && len(e.Children) > 0 {
		return w.fail("element", "invalid element normal form")
	}
	if e.Attrs != nil && len(e.Attrs) == 0 || e.XMLNS != nil && len(e.XMLNS) == 0 || e.Children != nil && len(e.Children) == 0 {
		return w.fail("element", "empty containers must be omitted")
	}
	if _, ok := e.XMLNS["xml"]; ok {
		return w.fail("namespace", "the xml binding is implicit")
	}
	if depth >= w.opts.MaxDepth {
		return w.limit("max_depth")
	}
	hasElement := false
	lastText := false
	mixed := false
	for _, v := range e.Children {
		switch c := v.(type) {
		case string:
			if c == "" || lastText {
				return w.fail("element", "invalid text child")
			}
			lastText = true
			mixed = true
		case *Element:
			if c == nil {
				return w.fail("element", "nil child element")
			}
			hasElement = true
			lastText = false
		default:
			return w.fail("element", "invalid child type")
		}
	}
	if len(e.Children) > 0 && !hasElement {
		return w.fail("element", "text-only content must use Text")
	}
	var declarations []Binding
	if len(e.XMLNS) > 0 {
		declarations = make([]Binding, 0, len(e.XMLNS))
		for p, u := range e.XMLNS {
			declarations = append(declarations, Binding{Prefix: p, URI: u})
		}
	}
	err := w.open(e.Name, e.NS, w.attrValues(e.Attrs), declarations, mixed || e.Text != "")
	clear(w.values)
	if err != nil {
		return err
	}
	if e.Text != "" {
		if err := w.Text(e.Text); err != nil {
			return err
		}
	}
	for _, v := range e.Children {
		switch c := v.(type) {
		case string:
			if err := w.Text(c); err != nil {
				return err
			}
		case *Element:
			if err := w.element(c, depth+1); err != nil {
				return err
			}
		}
	}
	return w.EndElement()
}

// Encode serializes a normalized element tree.
func Encode(e *Element, opts EncodeOptions) ([]byte, error) {
	w, err := NewEncoder(opts)
	if err != nil {
		return nil, err
	}
	if err = w.Element(e); err != nil {
		return nil, err
	}
	if err = w.Finish(); err != nil {
		return nil, err
	}
	return w.Drain(), nil
}
