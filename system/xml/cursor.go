// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"errors"
	"io"
	"math"
)

// Cursor delivers decoder events from an owned read-ahead batch. Only Fill and the
// completion of an element that extends past the batch read the source, so callers
// can run those apart from event consumption. Event slices are valid until the next
// cursor call.
type Cursor struct {
	err     error
	pending error
	dec     *Decoder
	read    batchCursor
	ns      namespaces
	attrs   []Attribute
	xmlns   []Binding
	scanA   []Attribute
	scanB   []Binding
	marks   []int
	path    []string
	scope   []Binding
	batch   Batch
	ev      Event
	scan    Event
	left    int
	budget  int
}

// NewCursor creates a cursor over d that refills up to budget bytes at a time.
func NewCursor(d *Decoder, budget int) (*Cursor, error) {
	if d == nil || budget < 1 {
		return nil, problem("argument", "decoder and positive budget are required")
	}
	c := &Cursor{dec: d, budget: budget}
	c.read.batch = &c.batch
	return c, nil
}

// Abort makes the cursor terminal with err, for a failure outside it such as the writer
// a copy wrote to, unless it has already failed; it returns the terminal error.
func (c *Cursor) Abort(err error) error {
	if c.err == nil {
		c.err = err
	}
	return c.err
}

// Err returns the failure that made the cursor terminal, or nil.
func (c *Cursor) Err() error { return c.err }

// Current returns the current event; its Kind is zero before the first event and after the document.
func (c *Cursor) Current() *Event { return &c.ev }

// Scope returns the namespace bindings in effect at the current start event.
func (c *Cursor) Scope() []Binding { return c.ns.visible(make([]Binding, 0, c.ns.len())) }

// NeedsInput reports whether the next event requires reading the source.
func (c *Cursor) NeedsInput() bool { return c.err == nil && c.pending == nil && c.left == 0 }

// Next advances to the next buffered event and reports false once the buffer is exhausted.
func (c *Cursor) Next() (*Event, bool) {
	if c.err != nil || c.left == 0 {
		return nil, false
	}
	c.attrs, c.xmlns = c.read.event(&c.ev, c.attrs, c.xmlns)
	c.left--
	c.track()
	return &c.ev, true
}

// track updates namespace scope and element path for the current event.
func (c *Cursor) track() {
	switch c.ev.Kind {
	case StartEvent:
		c.marks = append(c.marks[:c.ev.Depth-1], c.ns.len())
		c.path = append(c.path[:c.ev.Depth-1], c.ev.Name)
		for _, b := range c.ev.XMLNS {
			c.ns.push(b)
		}
	case EndEvent:
		c.ns.truncate(c.marks[c.ev.Depth])
		c.marks = c.marks[:c.ev.Depth]
		clear(c.path[c.ev.Depth:])
		c.path = c.path[:c.ev.Depth]
	}
}

// Fill reads ahead once the buffer is exhausted. It returns io.EOF after the document;
// a decoder failure follows the events read before it.
func (c *Cursor) Fill() error {
	if c.err != nil {
		return c.err
	}
	if c.left > 0 {
		return problem("state", "buffered events remain")
	}
	c.ev = Event{}
	if c.pending != nil {
		return c.deliver(c.pending)
	}
	c.reset()
	n, err := c.dec.AppendEvents(&c.batch, math.MaxInt, c.budget)
	c.left = n
	if err != nil {
		if n > 0 {
			c.pending = err
			return nil
		}
		return c.deliver(err)
	}
	return nil
}

func (c *Cursor) reset() {
	c.batch.Reset()
	c.read.offset = 0
	c.left = 0
}

func (c *Cursor) deliver(err error) error {
	if errors.Is(err, io.EOF) {
		c.pending = io.EOF
		return io.EOF
	}
	c.err = err
	return err
}

// Buffered reports whether the current start element ends inside the buffer.
func (c *Cursor) Buffered() bool {
	if c.err != nil || c.ev.Kind != StartEvent {
		return false
	}
	depth := c.ev.Depth - 1
	r := c.read
	for range c.left {
		c.scanA, c.scanB = r.event(&c.scan, c.scanA, c.scanB)
		if c.scan.Kind == EndEvent && c.scan.Depth == depth {
			return true
		}
	}
	return false
}

func (c *Cursor) startOperation() error {
	if c.err != nil {
		return c.err
	}
	if c.ev.Kind != StartEvent {
		return problem("state", "operation requires a start event")
	}
	return nil
}

// continued prepares the decoder to finish an element past the buffer.
func (c *Cursor) continued() error {
	if c.pending != nil {
		c.err = c.pending
		return c.err
	}
	c.reset()
	return nil
}

// settle makes the decoder's end event current after it finished an element.
func (c *Cursor) settle(err error) error {
	if err != nil {
		c.err = err
		return err
	}
	c.ev = c.dec.current
	c.track()
	return nil
}

func (c *Cursor) limit(ev *Event) error {
	c.err = failure("limit", "max_node_bytes exceeded", "max_node_bytes", ev.Pos, append([]string(nil), c.path...))
	return c.err
}

// Node appends the current element with its full scope and leaves the cursor on its end event.
func (c *Cursor) Node(b *Batch) error {
	if b == nil {
		return problem("argument", "batch is required")
	}
	if err := c.startOperation(); err != nil {
		return err
	}
	first := c.ev
	clear(c.scope)
	c.scope = c.ns.visible(c.scope[:0])
	first.XMLNS = c.scope
	start := first.Pos.Offset
	depth := first.Depth - 1
	limit := int64(c.dec.opts.MaxNodeBytes)
	if first.EndOffset-start > limit {
		return c.limit(&c.ev)
	}
	b.append(first)
	for {
		ev, ok := c.Next()
		if !ok {
			break
		}
		if ev.EndOffset-start > limit {
			return c.limit(ev)
		}
		b.append(*ev)
		if ev.Kind == EndEvent && ev.Depth == depth {
			return nil
		}
	}
	if err := c.continued(); err != nil {
		return err
	}
	return c.settle(c.dec.AppendThrough(b, depth, start))
}

// Skip discards the current element and leaves the cursor on its end event.
func (c *Cursor) Skip() error {
	if err := c.startOperation(); err != nil {
		return err
	}
	depth := c.ev.Depth - 1
	for {
		ev, ok := c.Next()
		if !ok {
			break
		}
		if ev.Kind == EndEvent && ev.Depth == depth {
			return nil
		}
	}
	if err := c.continued(); err != nil {
		return err
	}
	return c.settle(c.dec.SkipThrough(depth))
}

// CopyTo serializes the current element, declaring the namespaces it inherits, and leaves
// the cursor on its end event. drain runs after each event; its failure is terminal on w.
func (c *Cursor) CopyTo(w *Encoder, drain func(*Encoder) error) error {
	if w == nil {
		return problem("argument", "writer is required")
	}
	if err := c.startOperation(); err != nil {
		return err
	}
	depth := c.ev.Depth - 1
	clear(c.scope)
	c.scope = c.ns.visible(c.scope[:0])
	if err := w.writeEvent(&c.ev, c.scope); err != nil {
		c.err = err
		return err
	}
	if err := drained(w, drain); err != nil {
		c.err = err
		return err
	}
	for {
		ev, ok := c.Next()
		if !ok {
			break
		}
		err := w.writeEvent(ev, nil)
		if err == nil {
			err = drained(w, drain)
		}
		if err != nil {
			c.err = err
			return err
		}
		if ev.Kind == EndEvent && ev.Depth == depth {
			return nil
		}
	}
	if err := c.continued(); err != nil {
		return err
	}
	return c.settle(c.dec.CopyThrough(w, depth, drain))
}
