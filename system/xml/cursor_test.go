// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

func signature(ev *Event, scope []Binding) string {
	var s strings.Builder
	fmt.Fprintf(&s, "%d|%s|%s|%q|%d|%d:%d:%d|%d", ev.Kind, ev.Name, ev.NS, ev.Text, ev.Depth, ev.Pos.Line, ev.Pos.Column, ev.Pos.Offset, ev.EndOffset)
	for _, a := range ev.Attrs {
		fmt.Fprintf(&s, "|%s=%q", AttrKey(a.NS, a.Name), a.Value)
	}
	for _, b := range ev.XMLNS {
		fmt.Fprintf(&s, "|xmlns:%s=%s", b.Prefix, b.URI)
	}
	if scope != nil {
		fmt.Fprintf(&s, "|scope=%v", scope)
	}
	return s.String()
}

const cursorDocument = `<?xml version="1.0"?><!--c--><r xmlns="urn:d" xmlns:p="urn:p" a="1">` +
	`<p:item id="1" p:k="v"><name>One</name><p:x xmlns:p="urn:q"><p:y/></p:x>tail &amp; text</p:item>` +
	`<?pi data?><item id="2"/><item id="3"><deep><deeper><deepest>z</deepest></deeper></deep></item>` +
	`<other xmlns=""><item id="4">t<![CDATA[<c>]]></item></other></r><!--after-->`

func cursorReaders(src string) map[string]func() io.Reader {
	return map[string]func() io.Reader{
		"whole": func() io.Reader { return strings.NewReader(src) },
		"byte":  func() io.Reader { return &shortReader{r: strings.NewReader(src)} },
	}
}

func decoderEvents(t *testing.T, r io.Reader, opts Options) []string {
	t.Helper()
	d, err := NewDecoder(context.Background(), r, opts)
	require.NoError(t, err)
	var out []string
	for {
		ev, err := d.NextEvent()
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
		var scope []Binding
		if ev.Kind == StartEvent {
			scope = d.Scope()
		}
		out = append(out, signature(ev, scope))
	}
}

func newTestCursor(t *testing.T, r io.Reader, opts Options, budget int) *Cursor {
	t.Helper()
	d, err := NewDecoder(context.Background(), r, opts)
	require.NoError(t, err)
	c, err := NewCursor(d, budget)
	require.NoError(t, err)
	return c
}

// advance returns the next cursor event, refilling the buffer when needed.
func advance(t *testing.T, c *Cursor) (*Event, bool) {
	t.Helper()
	for {
		if ev, ok := c.Next(); ok {
			return ev, true
		}
		err := c.Fill()
		if errors.Is(err, io.EOF) {
			return nil, false
		}
		require.NoError(t, err)
	}
}

func TestCursorMatchesDecoder(t *testing.T) {
	opts := Options{Comments: true, Instructions: true}
	for name, reader := range cursorReaders(cursorDocument) {
		want := decoderEvents(t, reader(), opts)
		for _, budget := range []int{1, 7, 64, 1 << 16} {
			t.Run(name+"/"+strconv.Itoa(budget), func(t *testing.T) {
				c := newTestCursor(t, reader(), opts, budget)
				var got []string
				for {
					ev, ok := advance(t, c)
					if !ok {
						break
					}
					var scope []Binding
					if ev.Kind == StartEvent {
						scope = c.Scope()
					}
					got = append(got, signature(ev, scope))
					assert.Same(t, ev, c.Current())
				}
				assert.Equal(t, want, got)
				assert.Zero(t, c.Current().Kind, "no current event after the document")
				assert.False(t, c.NeedsInput())
				require.ErrorIs(t, c.Fill(), io.EOF, "end of document is stable")
			})
		}
	}
}

func TestCursorFillRequiresExhaustedBuffer(t *testing.T) {
	c := newTestCursor(t, strings.NewReader("<a><b/></a>"), Options{}, 1<<16)
	assert.True(t, c.NeedsInput())
	require.NoError(t, c.Fill())
	assert.False(t, c.NeedsInput())
	codeIs(t, c.Fill(), "state")
	_, err := NewCursor(nil, 1)
	code(t, err, "argument")
	d, err := NewDecoder(context.Background(), strings.NewReader("<a/>"), Options{})
	require.NoError(t, err)
	_, err = NewCursor(d, 0)
	code(t, err, "argument")
}

// walkRecords runs op on every start event at depth two and records what follows it.
func walkRecords(t *testing.T, c *Cursor, op func(*Cursor) string) []string {
	t.Helper()
	var out []string
	for {
		ev, ok := advance(t, c)
		if !ok {
			return out
		}
		if ev.Kind == StartEvent && ev.Depth == 2 {
			out = append(out, op(c))
			cur := c.Current()
			out = append(out, "at:"+signature(cur, nil))
		}
	}
}

func decoderRecords(t *testing.T, src string, opts Options, op func(*Decoder) string) []string {
	t.Helper()
	d, err := NewDecoder(context.Background(), strings.NewReader(src), opts)
	require.NoError(t, err)
	var out []string
	for {
		ev, err := d.NextEvent()
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
		if ev.Kind == StartEvent && ev.Depth == 2 {
			out = append(out, op(d))
			out = append(out, "at:"+signature(&d.current, nil))
		}
	}
}

func TestCursorNodeSkipCopyMatchDecoder(t *testing.T) {
	opts := Options{Comments: true, Instructions: true}
	node := func(b *Batch) string {
		trees, err := b.Elements()
		require.NoError(t, err)
		require.Len(t, trees, 1)
		return dump(trees[0])
	}
	wantNodes := decoderRecords(t, cursorDocument, opts, func(d *Decoder) string {
		var b Batch
		require.NoError(t, d.AppendNode(&b))
		return node(&b)
	})
	wantSkips := decoderRecords(t, cursorDocument, opts, func(d *Decoder) string {
		require.NoError(t, d.Skip())
		return "skip"
	})
	wantCopies := decoderRecords(t, cursorDocument, opts, func(d *Decoder) string {
		w, err := NewEncoder(EncodeOptions{Indent: " "})
		require.NoError(t, err)
		require.NoError(t, d.Copy(w))
		return string(w.Drain())
	})
	for name, reader := range cursorReaders(cursorDocument) {
		for _, budget := range []int{1, 13, 64, 1 << 16} {
			t.Run(name+"/"+strconv.Itoa(budget), func(t *testing.T) {
				got := walkRecords(t, newTestCursor(t, reader(), opts, budget), func(c *Cursor) string {
					var b Batch
					require.NoError(t, c.Node(&b))
					return node(&b)
				})
				assert.Equal(t, wantNodes, got)
				got = walkRecords(t, newTestCursor(t, reader(), opts, budget), func(c *Cursor) string {
					require.NoError(t, c.Skip())
					return "skip"
				})
				assert.Equal(t, wantSkips, got)
				got = walkRecords(t, newTestCursor(t, reader(), opts, budget), func(c *Cursor) string {
					w, err := NewEncoder(EncodeOptions{Indent: " "})
					require.NoError(t, err)
					drained := 0
					require.NoError(t, c.CopyTo(w, func(w *Encoder) error { drained++; return nil }))
					assert.Positive(t, drained)
					return string(w.Drain())
				})
				assert.Equal(t, wantCopies, got)
			})
		}
	}
}

func TestCursorBuffered(t *testing.T) {
	c := newTestCursor(t, strings.NewReader(`<r><a><b/></a><c>`+strings.Repeat("<d/>", 50)+`</c></r>`), Options{}, 1<<16)
	ev, ok := advance(t, c)
	require.True(t, ok)
	assert.Equal(t, "r", ev.Name)
	assert.True(t, c.Buffered(), "the whole document fits the buffer")

	c = newTestCursor(t, strings.NewReader(`<r><a><b/></a><c>`+strings.Repeat("<d/>", 50)+`</c></r>`), Options{}, 40)
	for {
		ev, ok = advance(t, c)
		require.True(t, ok)
		if ev.Kind == StartEvent && ev.Name == "c" {
			break
		}
	}
	assert.False(t, c.Buffered(), "the element ends beyond the buffer")
	require.NoError(t, c.Skip())
	assert.Equal(t, EndEvent, c.Current().Kind)
	assert.Equal(t, "c", c.Current().Name)
	ev, ok = advance(t, c)
	require.True(t, ok)
	assert.Equal(t, EndEvent, ev.Kind)
	assert.Equal(t, "r", ev.Name)
	assert.False(t, c.Buffered(), "only start events can be buffered")
}

func TestCursorStateAndLimitErrors(t *testing.T) {
	c := newTestCursor(t, strings.NewReader(`<r><a>text</a></r>`), Options{}, 1<<16)
	var b Batch
	codeIs(t, c.Node(&b), "state")
	codeIs(t, c.Skip(), "state")
	w, err := NewEncoder(EncodeOptions{})
	require.NoError(t, err)
	codeIs(t, c.CopyTo(w, nil), "state")

	src := `<r><a>` + strings.Repeat("x", 200) + `</a><b>` + strings.Repeat("y", 50) + `</b></r>`
	for _, budget := range []int{8, 1 << 16} {
		c = newTestCursor(t, strings.NewReader(src), Options{MaxNodeBytes: 100}, budget)
		for {
			ev, ok := advance(t, c)
			require.True(t, ok)
			if ev.Kind == StartEvent && ev.Name == "a" {
				break
			}
		}
		b.Reset()
		err = c.Node(&b)
		code(t, err, "limit")
		var e apierror.Error
		require.ErrorAs(t, err, &e)
		assert.Equal(t, "max_node_bytes", detail(e, "limit"))
		assert.Equal(t, []string{"r", "a"}, detail(e, "path"))
		assert.Same(t, err, c.Node(&b), "a cursor error is sticky")
		assert.Same(t, err, c.Fill())
	}
}

func TestCursorPendingErrorFollowsBufferedEvents(t *testing.T) {
	c := newTestCursor(t, strings.NewReader(`<r><a/><b></r>`), Options{}, 1<<16)
	var kinds []Kind
	var err error
	for {
		ev, ok := c.Next()
		if ok {
			kinds = append(kinds, ev.Kind)
			continue
		}
		if !c.NeedsInput() && len(kinds) > 0 {
			err = c.Fill()
			break
		}
		err = c.Fill()
		if err != nil {
			break
		}
	}
	assert.Equal(t, []Kind{StartEvent, StartEvent, EndEvent, StartEvent}, kinds)
	code(t, err, "syntax")
	assert.Same(t, err, c.Fill())
}

func TestCursorSelectionGuard(t *testing.T) {
	d, err := NewDecoder(context.Background(), strings.NewReader("<a/>"), Options{})
	require.NoError(t, err)
	c, err := NewCursor(d, 64)
	require.NoError(t, err)
	p, err := CompilePath("a", nil)
	require.NoError(t, err)
	_, err = NewSelector(d, []Path{p})
	require.NoError(t, err)
	codeIs(t, c.Fill(), "state")
}
