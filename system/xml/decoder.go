// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"io"

	apierror "github.com/wippyai/runtime/api/error"
)

type parsedName struct {
	raw    string
	prefix string
	local  string
}

type frame struct {
	qname    string
	name     string
	ns       string
	scope    int
	preserve bool
}

// Decoder provides a forward-only XML cursor.
type Decoder struct {
	ctx              context.Context
	source           io.Reader
	reader           io.Reader
	err              apierror.Error
	readErr          error
	encoding         string
	interned         map[string]string
	names            map[string]parsedName
	buf              []byte
	scratch          []byte
	stack            []frame
	ns               namespaces
	attributes       []Attribute
	declarations     []Binding
	current          Event
	node             Event
	scope            []Binding
	recent           [recentNames]parsedName
	opts             Options
	pos              Position
	lo               int
	hi               int
	events           int
	root             bool
	closed           bool
	pop              bool
	empty            bool
	cdata            bool
	initialized      bool
	selected         bool
	selectionReading bool
	cr               bool
	runContent       bool
}

// NewDecoder creates a decoder with validated options.
func NewDecoder(ctx context.Context, r io.Reader, opts Options) (*Decoder, error) {
	if ctx == nil || r == nil {
		return nil, problem("argument", "context and reader are required")
	}
	o, err := opts.defaults()
	if err != nil {
		return nil, err
	}
	return &Decoder{ctx: ctx, source: r, opts: o, pos: Position{Line: 1, Column: 1}, interned: make(map[string]string), names: make(map[string]parsedName)}, nil
}

// SetContext binds the context checked before each read of the source.
func (d *Decoder) SetContext(ctx context.Context) error {
	if ctx == nil {
		return problem("argument", "context is required")
	}
	d.ctx = ctx
	return nil
}

// Current returns the current event with borrowed slices.
func (d *Decoder) Current() Event { return d.current }

// Close releases cursor storage without closing the source.
func (d *Decoder) Close() {
	d.closed = true
	d.buf = nil
	d.scratch = nil
	d.stack = nil
	d.ns.reset()
	d.attributes = nil
	d.declarations = nil
	d.interned = nil
	d.names = nil
	d.current = Event{}
	d.reader = nil
	d.source = nil
}

// Skip consumes the current element through its end event.
func (d *Decoder) Skip() error {
	if d.current.Kind != StartEvent {
		return d.fail("state", "skip requires a start event", "")
	}
	return d.SkipThrough(d.current.Depth - 1)
}

// SkipThrough consumes events through the end event that returns the cursor to depth.
func (d *Decoder) SkipThrough(depth int) error {
	if err := d.inside(depth); err != nil {
		return err
	}
	for {
		ev, err := d.NextEvent()
		if err != nil {
			return err
		}
		if ev.Kind == EndEvent && ev.Depth == depth {
			return nil
		}
	}
}

// inside checks that the cursor is within an open element whose end returns to depth.
func (d *Decoder) inside(depth int) error {
	if d.selected {
		return problem("state", "selection owns the decoder")
	}
	if depth < 0 || d.current.Kind == 0 || d.current.Depth <= depth {
		return d.fail("state", "no open element at that depth", "")
	}
	return nil
}
