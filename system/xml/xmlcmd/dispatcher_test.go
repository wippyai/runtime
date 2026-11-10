// SPDX-License-Identifier: MPL-2.0

package xmlcmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/runtime/resource"
	xmlapi "github.com/wippyai/runtime/api/xml"
	streamsys "github.com/wippyai/runtime/system/stream"
	"github.com/wippyai/runtime/system/xml"
)

type result struct {
	data any
	err  error
}

type receiver struct {
	ch chan result
}

func (r *receiver) CompleteYield(_ uint64, data any, err error) { r.ch <- result{data, err} }

func frame(t *testing.T) (context.Context, *resource.Table) {
	t.Helper()
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	store := resource.NewStore()
	require.NoError(t, resource.SetStore(ctx, store))
	t.Cleanup(func() {
		_ = store.Close()
		ctxapi.ReleaseFrameContext(fc)
	})
	return ctx, store.Table()
}

type harness struct {
	ctx      context.Context
	handlers map[dispatcher.CommandID]dispatcher.Handler
}

func newHarness(ctx context.Context, t *testing.T, started bool) *harness {
	t.Helper()
	d := NewDispatcher(WithWorkers(2))
	if started {
		require.NoError(t, d.Start(ctx))
		t.Cleanup(func() { _ = d.Stop(ctx) })
	}
	h := &harness{ctx: ctx, handlers: map[dispatcher.CommandID]dispatcher.Handler{}}
	d.RegisterAll(func(id dispatcher.CommandID, handler dispatcher.Handler) { h.handlers[id] = handler })
	return h
}

func (h *harness) do(t *testing.T, cmd dispatcher.Command) error {
	t.Helper()
	handler := h.handlers[cmd.CmdID()]
	require.NotNil(t, handler, "handler for %d", cmd.CmdID())
	r := &receiver{ch: make(chan result, 1)}
	require.NoError(t, handler.Handle(h.ctx, cmd, 7, r))
	select {
	case res := <-r.ch:
		assert.Nil(t, res.data)
		return res.err
	case <-time.After(5 * time.Second):
		t.Fatal("command did not complete")
		return nil
	}
}

func errCode(t *testing.T, err error) string {
	t.Helper()
	var e apierror.Error
	require.ErrorAs(t, err, &e)
	return e.Details().GetString("code", "")
}

type closingBuffer struct {
	bytes.Buffer
	closed bool
}

func (b *closingBuffer) Close() error { b.closed = true; return nil }

func TestRegisterAll(t *testing.T) {
	ids := map[dispatcher.CommandID]bool{}
	NewDispatcher().RegisterAll(func(id dispatcher.CommandID, _ dispatcher.Handler) { ids[id] = true })
	assert.Equal(t, map[dispatcher.CommandID]bool{
		xmlapi.ReaderFill: true, xmlapi.ReaderNode: true, xmlapi.ReaderSkip: true,
		xmlapi.ReaderCopy: true, xmlapi.RecordsRead: true, xmlapi.WriterFlush: true,
	}, ids)
}

func TestReaderCommandsOverStream(t *testing.T) {
	for _, started := range []bool{false, true} {
		ctx, table := frame(t)
		h := newHarness(ctx, t, started)
		src := `<r><a id="1">` + strings.Repeat("<b>x</b>", 20000) + `</a><a id="2"><c/></a><a id="3"/></r>`
		sid := streamsys.Insert(table, io.NopCloser(strings.NewReader(src)))
		source, err := StreamSource(table, sid)
		require.NoError(t, err)
		r, err := NewReader(ctx, source, xml.Options{})
		require.NoError(t, err)
		rid := InsertReader(table, r)

		require.NoError(t, h.do(t, xmlapi.FillCmd{Reader: rid}))
		c := r.Cursor()
		ev, ok := c.Next()
		require.True(t, ok)
		assert.Equal(t, "r", ev.Name)
		ev, ok = c.Next()
		require.True(t, ok)
		assert.Equal(t, "a", ev.Name)
		require.False(t, c.Buffered(), "the first record exceeds the read-ahead budget")

		buf := &closingBuffer{}
		did := streamsys.Insert(table, buf)
		sink, flush, err := StreamSink(table, did)
		require.NoError(t, err)
		w, err := NewWriter(xml.EncodeOptions{}, sink, flush)
		require.NoError(t, err)
		wid := InsertWriter(table, w)
		require.NoError(t, h.do(t, xmlapi.CopyCmd{Reader: rid, Writer: wid}))
		assert.Equal(t, xml.EndEvent, c.Current().Kind)
		full, err := w.Write(func(*xml.Encoder) error { return nil })
		require.NoError(t, err)
		assert.False(t, full, "copy drains as the buffer fills")
		_, err = w.Finish()
		require.NoError(t, err)
		require.NoError(t, h.do(t, xmlapi.FlushCmd{Writer: wid, Sync: true}))
		assert.Equal(t, `<a id="1">`+strings.Repeat("<b>x</b>", 20000)+`</a>`, buf.String())
		assert.False(t, buf.closed, "writers leave the stream open")

		for {
			ev, ok = c.Next()
			if !ok {
				require.NoError(t, h.do(t, xmlapi.FillCmd{Reader: rid}))
				continue
			}
			if ev.Kind == xml.StartEvent {
				break
			}
		}
		require.NoError(t, h.do(t, xmlapi.NodeCmd{Reader: rid}))
		var trees []*xml.Element
		require.NoError(t, r.ViewNode(func(b *xml.Batch) error { trees, err = b.Elements(); return err }))
		assert.Equal(t, "2", trees[0].Attrs["id"])

		_, ok = c.Next()
		require.True(t, ok)
		require.NoError(t, h.do(t, xmlapi.SkipCmd{Reader: rid}))
		assert.Equal(t, xml.EndEvent, c.Current().Kind)

		r.Close()
		assert.Equal(t, "state", errCode(t, h.do(t, xmlapi.FillCmd{Reader: rid})))
		_, removed := table.Remove(resource.Handle(rid))
		assert.True(t, removed)
		assert.Equal(t, "state", errCode(t, h.do(t, xmlapi.FillCmd{Reader: rid})), "a removed reader is not found")
	}
}

func TestRecordsCommand(t *testing.T) {
	ctx, table := frame(t)
	h := newHarness(ctx, t, true)
	r, err := NewReader(ctx, strings.NewReader(`<r><i n="1"/><x/><i n="2"/><i n="3"/></r>`), xml.Options{})
	require.NoError(t, err)
	rid := InsertReader(table, r)
	assert.Equal(t, "state", errCode(t, h.do(t, xmlapi.RecordsReadCmd{Reader: rid, Count: 2})))
	p, err := xml.CompilePath("r/i", nil)
	require.NoError(t, err)
	require.NoError(t, r.Select([]xml.Path{p}))
	require.NoError(t, h.do(t, xmlapi.RecordsReadCmd{Reader: rid, Count: 2}))
	var trees []*xml.Element
	require.NoError(t, r.ViewRecords(func(b *xml.Batch) error { trees, err = b.Elements(); return err }))
	assert.Len(t, trees, 2)
	require.NoError(t, h.do(t, xmlapi.RecordsReadCmd{Reader: rid, Count: 2}))
	require.NoError(t, r.ViewRecords(func(b *xml.Batch) error { trees, err = b.Elements(); return err }))
	require.Len(t, trees, 1)
	assert.Equal(t, "3", trees[0].Attrs["n"])
	require.ErrorIs(t, h.do(t, xmlapi.RecordsReadCmd{Reader: rid, Count: 2}), io.EOF)
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestFlushFailureIsTerminal(t *testing.T) {
	ctx, table := frame(t)
	h := newHarness(ctx, t, true)
	w, err := NewWriter(xml.EncodeOptions{}, failingWriter{err: errors.New("disk full")}, nil)
	require.NoError(t, err)
	wid := InsertWriter(table, w)
	_, err = w.Write(func(e *xml.Encoder) error { return e.StartElement("a", nil, "") })
	require.NoError(t, err)
	first := h.do(t, xmlapi.FlushCmd{Writer: wid})
	require.Error(t, first)
	assert.Equal(t, "io", errCode(t, first))
	_, again := w.Write((*xml.Encoder).EndElement)
	assert.Same(t, first, again)
	assert.Same(t, first, h.do(t, xmlapi.FlushCmd{Writer: wid}))
}

func TestCanceledCommand(t *testing.T) {
	ctx, table := frame(t)
	r, err := NewReader(ctx, strings.NewReader(`<r>`+strings.Repeat("<a/>", 50000)+`</r>`), xml.Options{})
	require.NoError(t, err)
	rid := InsertReader(table, r)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	h := newHarness(canceled, t, false)
	err = h.do(t, xmlapi.FillCmd{Reader: rid})
	var e apierror.Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, apierror.Canceled, e.Kind())
}

func TestNoTable(t *testing.T) {
	h := newHarness(context.Background(), t, false)
	assert.Equal(t, "io", errCode(t, h.do(t, xmlapi.FillCmd{Reader: 1})))
}

// blockingReader blocks reads until released, standing in for a slow network source.
type blockingReader struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingReader) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return 0, io.EOF
}

func TestDropDuringCommandDoesNotBlock(t *testing.T) {
	ctx, table := frame(t)
	src := &blockingReader{started: make(chan struct{}), release: make(chan struct{})}
	r, err := NewReader(ctx, src, xml.Options{})
	require.NoError(t, err)
	rid := InsertReader(table, r)
	h := newHarness(ctx, t, true)
	done := &receiver{ch: make(chan result, 1)}
	require.NoError(t, h.handlers[xmlapi.ReaderFill].Handle(ctx, xmlapi.FillCmd{Reader: rid}, 1, done))
	<-src.started
	dropped := make(chan struct{})
	go func() {
		table.Reset()
		close(dropped)
	}()
	select {
	case <-dropped:
	case <-time.After(2 * time.Second):
		t.Fatal("table reset blocked on a reader in use")
	}
	assert.True(t, r.Closed())
	close(src.release)
	res := <-done.ch
	require.Error(t, res.err)
}
