// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"bytes"
	"context"
	"strings"
)

// Node materializes the current element through batch replay.
func (d *Decoder) Node() (*Element, error) {
	var b Batch
	if err := d.AppendNode(&b); err != nil {
		return nil, err
	}
	trees, err := b.Elements()
	if err != nil {
		return nil, err
	}
	return trees[0], nil
}

type treeFrame struct {
	element *Element
	pending string
	text    strings.Builder
}

func (f *treeFrame) takeText() string {
	if f.text.Len() == 0 {
		s := f.pending
		f.pending = ""
		return s
	}
	s := f.text.String()
	f.text.Reset()
	return s
}

func (f *treeFrame) flushText() {
	if f.text.Len() > 0 || f.pending != "" {
		f.element.Children = append(f.element.Children, f.takeText())
	}
}

// Elements materializes complete trees through event replay.
func (b *Batch) Elements() ([]*Element, error) {
	var roots []*Element
	var stack []treeFrame
	values := make(map[string]string)
	intern := func(p []byte) string {
		if len(p) > maxInternBytes {
			return string(p)
		}
		if s, ok := values[string(p)]; ok {
			return s
		}
		s := string(p)
		if len(values) < maxInternEntries {
			values[s] = s
		}
		return s
	}
	err := b.Replay(func(ev Event) error {
		switch ev.Kind {
		case StartEvent:
			e := &Element{Name: ev.Name, NS: ev.NS}
			if len(ev.Attrs) > 0 {
				e.Attrs = make(map[string]string, len(ev.Attrs))
				for _, a := range ev.Attrs {
					e.Attrs[AttrKey(a.NS, a.Name)] = intern(a.Value)
				}
			}
			if len(ev.XMLNS) > 0 {
				e.XMLNS = make(map[string]string, len(ev.XMLNS))
				for _, n := range ev.XMLNS {
					e.XMLNS[n.Prefix] = n.URI
				}
			}
			if len(stack) == 0 {
				roots = append(roots, e)
			} else {
				p := &stack[len(stack)-1]
				p.flushText()
				p.element.Children = append(p.element.Children, e)
			}
			stack = append(stack, treeFrame{element: e})
		case TextEvent:
			if len(stack) > 0 {
				f := &stack[len(stack)-1]
				if f.pending == "" && f.text.Len() == 0 && len(ev.Text) <= maxInternBytes {
					f.pending = intern(ev.Text)
				} else {
					if f.pending != "" {
						f.text.WriteString(f.pending)
						f.pending = ""
					}
					_, _ = f.text.Write(ev.Text)
				}
			}
		case EndEvent:
			if len(stack) == 0 {
				return problem("state", "unbalanced batch")
			}
			top := &stack[len(stack)-1]
			e := top.element
			if e.Name != ev.Name || e.NS != ev.NS {
				return problem("state", "mismatched batch end")
			}
			if len(e.Children) == 0 {
				e.Text = top.takeText()
			} else {
				top.flushText()
			}
			top.text.Reset()
			stack = stack[:len(stack)-1]
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(stack) > 0 {
		return nil, problem("state", "incomplete batch")
	}
	return roots, nil
}

// Decode parses a complete XML document into a normalized tree.
func Decode(data []byte, opts Options) (*Element, error) {
	d, err := NewDecoder(context.Background(), bytes.NewReader(data), opts)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	var b Batch
	if err = d.AppendDocument(&b); err != nil {
		return nil, err
	}
	trees, err := b.Elements()
	if err != nil {
		return nil, err
	}
	return trees[0], nil
}
