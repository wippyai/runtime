// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"encoding/binary"
	"errors"
	"io"
)

// Batch stores owned event bytes and a dictionary of immutable names.
// Reset permits reuse once all readers finish.
type Batch struct {
	recent    [recentNames]batchName
	index     map[string]uint64
	names     []string
	data      []byte
	count     int
	nameBytes int
}

// batchName caches a dictionary entry; ref is the index plus one, zero for an empty slot.
type batchName struct {
	s   string
	ref uint64
}

// Reset clears a batch while retaining its allocated storage.
func (b *Batch) Reset() {
	clear(b.recent[:])
	b.data = b.data[:0]
	clear(b.names)
	b.names = b.names[:0]
	clear(b.index)
	b.count = 0
	b.nameBytes = 0
}

// Len returns the number of stored events.
func (b *Batch) Len() int { return b.count }

// Size returns the bytes held by the stored events and their name dictionary.
func (b *Batch) Size() int { return len(b.data) + b.nameBytes }

func (b *Batch) number(n int) { b.data = binary.AppendUvarint(b.data, uint64(n)) }

func (b *Batch) name(s string) {
	slot := &b.recent[0]
	if s != "" {
		slot = &b.recent[(len(s)*31+int(s[0])*7+int(s[len(s)-1]))&(recentNames-1)]
	}
	if slot.ref != 0 && slot.s == s {
		b.data = binary.AppendUvarint(b.data, slot.ref-1)
		return
	}
	if b.index == nil {
		b.index = make(map[string]uint64)
	}
	n, ok := b.index[s]
	if !ok {
		n = uint64(len(b.names))
		b.names = append(b.names, s)
		b.index[s] = n
		b.nameBytes += len(s)
	}
	*slot = batchName{s: s, ref: n + 1}
	b.data = binary.AppendUvarint(b.data, n)
}

func (b *Batch) value(p []byte) {
	b.number(len(p))
	b.data = append(b.data, p...)
}

func (b *Batch) append(ev Event) {
	b.data = append(b.data, byte(ev.Kind))
	b.number(ev.Depth)
	b.data = binary.AppendUvarint(b.data, uint64(ev.Pos.Offset))
	b.number(ev.Pos.Line)
	b.number(ev.Pos.Column)
	b.data = binary.AppendUvarint(b.data, uint64(ev.EndOffset))
	switch ev.Kind {
	case StartEvent, EndEvent:
		b.name(ev.Name)
		b.name(ev.NS)
	case PIEvent:
		b.name(ev.Name)
		b.value(ev.Text)
	case TextEvent, CommentEvent:
		b.value(ev.Text)
	}
	if ev.Kind == StartEvent {
		b.number(len(ev.Attrs))
		for _, a := range ev.Attrs {
			b.name(a.Name)
			b.name(a.NS)
			b.value(a.Value)
		}
		b.number(len(ev.XMLNS))
		for _, ns := range ev.XMLNS {
			b.name(ns.Prefix)
			b.name(ns.URI)
		}
	}

	b.count++
}

type batchCursor struct {
	batch  *Batch
	offset int
}

func (c *batchCursor) number() uint64 {
	v, n := binary.Uvarint(c.batch.data[c.offset:])
	c.offset += n
	return v
}

func (c *batchCursor) name() string { return c.batch.names[c.number()] }

func (c *batchCursor) value() []byte {
	n := int(c.number())
	p := c.batch.data[c.offset : c.offset+n]
	c.offset += n
	return p
}

// event decodes the next stored event into ev, reusing the attribute and binding storage.
func (c *batchCursor) event(ev *Event, attrs []Attribute, bindings []Binding) ([]Attribute, []Binding) {
	kind := Kind(c.batch.data[c.offset])
	c.offset++
	*ev = Event{Kind: kind, Depth: int(c.number())}
	ev.Pos = Position{Offset: int64(c.number()), Line: int(c.number()), Column: int(c.number())}
	ev.EndOffset = int64(c.number())
	switch kind {
	case StartEvent, EndEvent:
		ev.Name = c.name()
		ev.NS = c.name()
	case PIEvent:
		ev.Name = c.name()
		ev.Text = c.value()
	case TextEvent, CommentEvent:
		ev.Text = c.value()
	}
	if kind == StartEvent {
		attrs = attrs[:0]
		n := int(c.number())
		for range n {
			attrs = append(attrs, Attribute{Name: c.name(), NS: c.name(), Value: c.value()})
		}
		ev.Attrs = attrs
		bindings = bindings[:0]
		n = int(c.number())
		for range n {
			bindings = append(bindings, Binding{Prefix: c.name(), URI: c.name()})
		}
		ev.XMLNS = bindings
	}
	return attrs, bindings
}

// Replay provides borrowed event slices for the duration of each visitor call.
func (b *Batch) Replay(visit func(Event) error) error {
	if visit == nil {
		return problem("argument", "visitor is required")
	}
	c := batchCursor{batch: b}
	var attrs []Attribute
	var bindings []Binding
	var ev Event
	for range b.count {
		attrs, bindings = c.event(&ev, attrs, bindings)
		if err := visit(ev); err != nil {
			return err
		}
	}
	return nil
}

// AppendEvents appends up to n events, stopping once the batch holds at least budget bytes.
// At least one event is appended unless the decoder fails or the document ends.
// A partial batch remains valid at EOF.
func (d *Decoder) AppendEvents(b *Batch, n, budget int) (int, error) {
	if b == nil || n < 1 || budget < 1 {
		return 0, problem("argument", "batch, positive count and positive budget are required")
	}
	for i := 0; i < n; i++ {
		ev, err := d.NextEvent()
		if err != nil {
			return i, err
		}
		b.append(*ev)
		if b.Size() >= budget {
			return i + 1, nil
		}
	}
	return n, nil
}

// AppendNode appends the current element and leaves the cursor on its end event.
func (d *Decoder) AppendNode(b *Batch) error {
	if b == nil {
		return problem("argument", "batch is required")
	}
	return d.VisitNode(b.appendEvent)
}

// VisitNode passes the events of the current element to visit, the first carrying the
// complete namespace scope, and leaves the cursor on its end event. Event slices are
// borrowed for the duration of each call.
func (d *Decoder) VisitNode(visit func(*Event) error) error {
	if d.selected {
		return problem("state", "selection owns the decoder")
	}
	_, err := d.visitNode(visit)
	return err
}

func (b *Batch) appendEvent(ev *Event) error {
	b.append(*ev)
	return nil
}

// visitNode returns the bytes of the namespace scope the detached root carries.
func (d *Decoder) visitNode(visit func(*Event) error) (int, error) {
	if d.current.Kind != StartEvent {
		return 0, d.fail("state", "node requires a start event", "")
	}
	if d.current.EndOffset-d.current.Pos.Offset > int64(d.opts.MaxNodeBytes) {
		return 0, d.limit("max_node_bytes")
	}
	d.node = d.current
	clear(d.scope)
	d.scope = d.ns.visible(d.scope[:0])
	d.node.XMLNS = d.scope
	if err := visit(&d.node); err != nil {
		return 0, err
	}
	return scopeBytes(d.scope), d.visitThrough(d.node.Depth-1, d.node.Pos.Offset, visit)
}

// AppendThrough appends events through the end event that returns the cursor to depth.
// The span from start to each appended event is bounded by MaxNodeBytes.
func (d *Decoder) AppendThrough(b *Batch, depth int, start int64) error {
	if b == nil {
		return problem("argument", "batch is required")
	}
	if err := d.inside(depth); err != nil {
		return err
	}
	return d.visitThrough(depth, start, b.appendEvent)
}

func (d *Decoder) visitThrough(depth int, start int64, visit func(*Event) error) error {
	for {
		ev, err := d.NextEvent()
		if err != nil {
			return err
		}
		if ev.EndOffset-start > int64(d.opts.MaxNodeBytes) {
			return d.limit("max_node_bytes")
		}
		if err = visit(ev); err != nil {
			return err
		}
		if ev.Kind == EndEvent && ev.Depth == depth {
			return nil
		}
	}
}

func scopeBytes(scope []Binding) int {
	n := 0
	for _, b := range scope {
		n += len(b.Prefix) + len(b.URI)
	}
	return n
}

// AppendDocument appends the root element of a complete document and validates the input to its end.
func (d *Decoder) AppendDocument(b *Batch) error {
	if b == nil {
		return problem("argument", "batch is required")
	}
	return d.VisitDocument(b.appendEvent)
}

// VisitDocument visits the root element of a complete document like VisitNode and
// validates the input to its end.
func (d *Decoder) VisitDocument(visit func(*Event) error) error {
	for {
		ev, err := d.NextEvent()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if ev.Kind == StartEvent {
			if err = d.VisitNode(visit); err != nil {
				return err
			}
		}
	}
}
