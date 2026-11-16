// SPDX-License-Identifier: MPL-2.0

package xml

// Copy serializes the current element without building a tree.
func (d *Decoder) Copy(w *Encoder) error { return d.CopyTo(w, nil) }

// CopyTo calls drain after each event, allowing bounded output buffering.
// A drain failure becomes terminal on the encoder.
func (d *Decoder) CopyTo(w *Encoder, drain func(*Encoder) error) error {
	if w == nil {
		return problem("argument", "writer is required")
	}
	if d.current.Kind != StartEvent {
		return d.fail("state", "copy requires a start event", "")
	}
	if err := w.writeEvent(&d.current, d.Scope()); err != nil {
		return err
	}
	if err := drained(w, drain); err != nil {
		return err
	}
	return d.copyThrough(w, d.current.Depth-1, drain)
}

// CopyThrough serializes events through the end event that returns the cursor to depth.
func (d *Decoder) CopyThrough(w *Encoder, depth int, drain func(*Encoder) error) error {
	if w == nil {
		return problem("argument", "writer is required")
	}
	if err := d.inside(depth); err != nil {
		return err
	}
	return d.copyThrough(w, depth, drain)
}

func (d *Decoder) copyThrough(w *Encoder, depth int, drain func(*Encoder) error) error {
	for {
		ev, err := d.NextEvent()
		if err != nil {
			return err
		}
		if err = w.writeEvent(ev, nil); err != nil {
			return err
		}
		if err = drained(w, drain); err != nil {
			return err
		}
		if ev.Kind == EndEvent && ev.Depth == depth {
			return nil
		}
	}
}

func drained(w *Encoder, drain func(*Encoder) error) error {
	if drain == nil {
		return nil
	}
	if err := drain(w); err != nil {
		return w.Abort(err)
	}
	return nil
}

// writeEvent serializes a decoded event. A non-nil scope replaces the declarations of a
// start event that begins a copied subtree. Event values are trusted as decoder output.
func (w *Encoder) writeEvent(ev *Event, scope []Binding) error {
	switch ev.Kind {
	case StartEvent:
		if err := w.ready(); err != nil {
			return err
		}
		if len(w.stack) == 0 && w.root {
			return w.fail("state", "multiple root elements")
		}
		if len(w.stack) >= w.opts.MaxDepth {
			return w.limit("max_depth")
		}
		if len(ev.Attrs) > w.opts.MaxAttrs {
			return w.limit("max_attrs")
		}
		start := w.ns.len()
		if err := w.declareRoot(start); err != nil {
			return err
		}
		bindings := ev.XMLNS
		if scope != nil {
			bindings = scope
		}
		for _, b := range bindings {
			if err := w.declare(b.Prefix, b.URI, start); err != nil {
				return err
			}
		}
		as := w.attributes[:0]
		for _, a := range ev.Attrs {
			as = append(as, outputAttribute{name: a.Name, ns: a.NS, data: a.Value})
		}
		return w.startPrepared(ev.Name, ev.NS, start, as, true, false)
	case EndEvent:
		return w.EndElement()
	case TextEvent:
		if err := w.ready(); err != nil {
			return err
		}
		if err := admitText(w, ev.Text); err != nil {
			return err
		}
		w.content(true)
		return escape(w, ev.Text, false, true)
	case CommentEvent:
		return w.Comment(string(ev.Text))
	case PIEvent:
		return w.instruction(ev.Name, ev.Text)
	}
	return nil
}
