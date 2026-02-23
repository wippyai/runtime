// SPDX-License-Identifier: MPL-2.0

package stream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/runtime/resource"
	streamapi "github.com/wippyai/runtime/api/stream"
)

// --- Entry.Drop ---

func TestEntry_Drop(t *testing.T) {
	closed := false
	entry := &Entry{
		closer: &trackingCloser{Reader: strings.NewReader("test"), closed: &closed},
	}
	entry.Drop()
	assert.True(t, closed)
	assert.True(t, entry.closed.Load())
}

func TestEntry_Drop_AlreadyClosed(t *testing.T) {
	closed := false
	entry := &Entry{
		closer: &trackingCloser{Reader: strings.NewReader("test"), closed: &closed},
	}
	entry.closed.Store(true)
	entry.Drop()
	assert.False(t, closed) // should not close again
}

func TestEntry_Drop_NilCloser(t *testing.T) {
	entry := &Entry{}
	entry.Drop() // should not panic
}

// --- InsertWithSize ---

func TestInsertWithSize(t *testing.T) {
	table := resource.NewTable()
	stream := newRWSStream("data")
	id := InsertWithSize(table, stream, 42)

	size, _, _, err := Stat(table, id)
	require.NoError(t, err)
	assert.Equal(t, int64(42), size)
}

// --- ReadBuffered ---

func TestReadBuffered_Direct(t *testing.T) {
	table := resource.NewTable()
	data := "hello world"
	id := Insert(table, io.NopCloser(strings.NewReader(data)))

	buf, err := ReadBuffered(table, id, 5)
	require.NoError(t, err)
	require.NotNil(t, buf)
	defer buf.Release()

	assert.Equal(t, "hello", string(buf.Bytes()))
}

func TestReadBuffered_EOF(t *testing.T) {
	table := resource.NewTable()
	id := Insert(table, io.NopCloser(bytes.NewReader(nil)))

	buf, err := ReadBuffered(table, id, 10)
	assert.ErrorIs(t, err, io.EOF)
	assert.Nil(t, buf)
}

func TestReadBuffered_NotReadable(t *testing.T) {
	table := resource.NewTable()
	id := Insert(table, nopWriteCloser{bytes.NewBuffer(nil)})

	_, err := ReadBuffered(table, id, 10)
	assert.ErrorIs(t, err, streamapi.ErrNotReadable)
}

// --- Scanner split types ---

func TestCreateScanner_SplitWords(t *testing.T) {
	table := resource.NewTable()
	id := Insert(table, io.NopCloser(strings.NewReader("hello world foo")))

	scanID, err := CreateScanner(table, id, streamapi.SplitWords)
	require.NoError(t, err)

	result, err := ScanNext(table, scanID)
	require.NoError(t, err)
	assert.True(t, result.HasToken)
	assert.Equal(t, "hello", result.Text)

	result, _ = ScanNext(table, scanID)
	assert.Equal(t, "world", result.Text)

	result, _ = ScanNext(table, scanID)
	assert.Equal(t, "foo", result.Text)
}

func TestCreateScanner_SplitBytes(t *testing.T) {
	table := resource.NewTable()
	id := Insert(table, io.NopCloser(strings.NewReader("abc")))

	scanID, err := CreateScanner(table, id, streamapi.SplitBytes)
	require.NoError(t, err)

	result, _ := ScanNext(table, scanID)
	assert.Equal(t, "a", result.Text)

	result, _ = ScanNext(table, scanID)
	assert.Equal(t, "b", result.Text)

	result, _ = ScanNext(table, scanID)
	assert.Equal(t, "c", result.Text)
}

func TestCreateScanner_SplitRunes(t *testing.T) {
	table := resource.NewTable()
	id := Insert(table, io.NopCloser(strings.NewReader("ab")))

	scanID, err := CreateScanner(table, id, streamapi.SplitRunes)
	require.NoError(t, err)

	result, _ := ScanNext(table, scanID)
	assert.Equal(t, "a", result.Text)
}

// --- Dispatcher handler tests ---

type syncReceiver struct {
	data any
	err  error
	done chan struct{}
}

func newSyncReceiver() *syncReceiver {
	return &syncReceiver{done: make(chan struct{})}
}

func (r *syncReceiver) CompleteYield(_ uint64, data any, err error) {
	r.data = data
	r.err = err
	close(r.done)
}

func (r *syncReceiver) wait(t *testing.T) {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func setupDispatcher(t *testing.T) (context.Context, *resource.Store, map[dispatcher.CommandID]dispatcher.Handler) {
	t.Helper()
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	store := resource.NewStore()
	require.NoError(t, resource.SetStore(ctx, store))
	t.Cleanup(func() { _ = store.Close() })

	d := NewDispatcher()
	require.NoError(t, d.Start(ctx))
	t.Cleanup(func() { _ = d.Stop(ctx) })

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	return ctx, store, handlers
}

func TestDispatcher_WriteHandler(t *testing.T) {
	ctx, _, handlers := setupDispatcher(t)
	table := resource.GetTable(ctx)
	stream := newRWSStream("")
	id := Insert(table, stream)

	recv := newSyncReceiver()
	require.NoError(t, handlers[streamapi.Write].Handle(ctx, streamapi.WriteCmd{StreamID: id, Data: []byte("written")}, 0, recv))
	recv.wait(t)

	n, ok := recv.data.(int64)
	require.True(t, ok)
	assert.Equal(t, int64(7), n)
	assert.Equal(t, "written", stream.String())
}

func TestDispatcher_SeekHandler(t *testing.T) {
	ctx, _, handlers := setupDispatcher(t)
	table := resource.GetTable(ctx)
	stream := newRWSStream("test data")
	id := Insert(table, stream)

	recv := newSyncReceiver()
	require.NoError(t, handlers[streamapi.Seek].Handle(ctx, streamapi.SeekCmd{StreamID: id, Offset: 5, Whence: io.SeekStart}, 0, recv))
	recv.wait(t)

	pos, ok := recv.data.(int64)
	require.True(t, ok)
	assert.Equal(t, int64(5), pos)
}

func TestDispatcher_FlushHandler(t *testing.T) {
	ctx, _, handlers := setupDispatcher(t)
	table := resource.GetTable(ctx)
	stream := newRWSStream("")
	id := Insert(table, stream)

	recv := newSyncReceiver()
	require.NoError(t, handlers[streamapi.Flush].Handle(ctx, streamapi.FlushCmd{StreamID: id}, 0, recv))
	recv.wait(t)

	assert.Nil(t, recv.data)
	assert.True(t, stream.flushed)
}

func TestDispatcher_StatHandler(t *testing.T) {
	ctx, _, handlers := setupDispatcher(t)
	table := resource.GetTable(ctx)
	stream := newRWSStream("test data")
	id := InsertWithSize(table, stream, -1)

	recv := newSyncReceiver()
	require.NoError(t, handlers[streamapi.Stat].Handle(ctx, streamapi.StatCmd{StreamID: id}, 0, recv))
	recv.wait(t)

	info, ok := recv.data.(streamapi.Info)
	require.True(t, ok)
	assert.True(t, info.Readable)
	assert.True(t, info.Writable)
	assert.True(t, info.Seekable)
}

func TestDispatcher_ScannerCreateHandler(t *testing.T) {
	ctx, _, handlers := setupDispatcher(t)
	table := resource.GetTable(ctx)
	id := Insert(table, io.NopCloser(strings.NewReader("line1\nline2")))

	recv := newSyncReceiver()
	require.NoError(t, handlers[streamapi.ScannerCreate].Handle(ctx, streamapi.ScannerCreateCmd{StreamID: id, SplitType: streamapi.SplitLines}, 0, recv))
	recv.wait(t)

	scannerID, ok := recv.data.(uint64)
	require.True(t, ok)
	assert.NotZero(t, scannerID)
}

func TestDispatcher_ScannerScanHandler(t *testing.T) {
	ctx, _, handlers := setupDispatcher(t)
	table := resource.GetTable(ctx)
	id := Insert(table, io.NopCloser(strings.NewReader("line1\nline2")))

	scannerID, err := CreateScanner(table, id, streamapi.SplitLines)
	require.NoError(t, err)

	recv := newSyncReceiver()
	require.NoError(t, handlers[streamapi.ScannerScan].Handle(ctx, streamapi.ScannerScanCmd{ScannerID: scannerID}, 0, recv))
	recv.wait(t)

	result, ok := recv.data.(streamapi.ScanResult)
	require.True(t, ok)
	assert.True(t, result.HasToken)
	assert.Equal(t, "line1", result.Text)
}

// --- Dispatcher no table ---

func TestDispatcher_Execute_NoTable(t *testing.T) {
	// context without resource store
	ctx := context.Background()

	d := NewDispatcher()
	require.NoError(t, d.Start(ctx))
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	recv := newSyncReceiver()
	require.NoError(t, handlers[streamapi.Read].Handle(ctx, streamapi.ReadCmd{StreamID: 1, Size: 10}, 0, recv))
	recv.wait(t)

	assert.ErrorIs(t, recv.err, streamapi.ErrNoTable)
}

// --- Dispatcher handler errors ---

func TestDispatcher_WriteHandler_NotFound(t *testing.T) {
	ctx, _, handlers := setupDispatcher(t)

	recv := newSyncReceiver()
	require.NoError(t, handlers[streamapi.Write].Handle(ctx, streamapi.WriteCmd{StreamID: 999, Data: []byte("x")}, 0, recv))
	recv.wait(t)

	assert.True(t, errors.Is(recv.err, streamapi.ErrNotFound))
}

func TestDispatcher_SeekHandler_NotFound(t *testing.T) {
	ctx, _, handlers := setupDispatcher(t)

	recv := newSyncReceiver()
	require.NoError(t, handlers[streamapi.Seek].Handle(ctx, streamapi.SeekCmd{StreamID: 999}, 0, recv))
	recv.wait(t)

	assert.True(t, errors.Is(recv.err, streamapi.ErrNotFound))
}

func TestDispatcher_FlushHandler_NotFound(t *testing.T) {
	ctx, _, handlers := setupDispatcher(t)

	recv := newSyncReceiver()
	require.NoError(t, handlers[streamapi.Flush].Handle(ctx, streamapi.FlushCmd{StreamID: 999}, 0, recv))
	recv.wait(t)

	assert.True(t, errors.Is(recv.err, streamapi.ErrNotFound))
}

func TestDispatcher_StatHandler_NotFound(t *testing.T) {
	ctx, _, handlers := setupDispatcher(t)

	recv := newSyncReceiver()
	require.NoError(t, handlers[streamapi.Stat].Handle(ctx, streamapi.StatCmd{StreamID: 999}, 0, recv))
	recv.wait(t)

	assert.True(t, errors.Is(recv.err, streamapi.ErrNotFound))
}

func TestDispatcher_ScannerCreateHandler_NotFound(t *testing.T) {
	ctx, _, handlers := setupDispatcher(t)

	recv := newSyncReceiver()
	require.NoError(t, handlers[streamapi.ScannerCreate].Handle(ctx, streamapi.ScannerCreateCmd{StreamID: 999}, 0, recv))
	recv.wait(t)

	assert.True(t, errors.Is(recv.err, streamapi.ErrNotFound))
}

func TestDispatcher_ScannerScanHandler_NotFound(t *testing.T) {
	ctx, _, handlers := setupDispatcher(t)

	recv := newSyncReceiver()
	require.NoError(t, handlers[streamapi.ScannerScan].Handle(ctx, streamapi.ScannerScanCmd{ScannerID: 999}, 0, recv))
	recv.wait(t)

	assert.True(t, errors.Is(recv.err, streamapi.ErrScannerNotFound))
}

// --- WithWorkers zero/negative ---

func TestWithWorkers_Zero(t *testing.T) {
	d := NewDispatcher(WithWorkers(0))
	assert.Equal(t, 16, d.workers) // unchanged from default
}

func TestWithWorkers_Negative(t *testing.T) {
	d := NewDispatcher(WithWorkers(-1))
	assert.Equal(t, 16, d.workers)
}

// --- Stat without stater ---

func TestStat_NoStater(t *testing.T) {
	table := resource.NewTable()
	reader := io.NopCloser(strings.NewReader("test"))
	id := InsertWithSize(table, reader, -1)

	size, pos, caps, err := Stat(table, id)
	require.NoError(t, err)
	assert.Equal(t, int64(-1), size) // no stater, size stays -1
	assert.Equal(t, int64(-1), pos)  // no seeker
	assert.True(t, caps.Readable)
	assert.False(t, caps.Writable)
	assert.False(t, caps.Seekable)
}

// --- Close without closer ---

func TestClose_NoCloser(t *testing.T) {
	table := resource.NewTable()
	// insert entry directly with nil closer
	entry := &Entry{
		reader: strings.NewReader("test"),
		caps:   Capabilities{Readable: true},
	}
	id := uint64(table.Insert(TypeStream, entry))

	err := Close(table, id)
	assert.NoError(t, err)
}

// --- Close already closed via Drop ---

func TestClose_AlreadyClosedViaEntry(t *testing.T) {
	table := resource.NewTable()
	closed := false
	stream := &trackingCloser{Reader: strings.NewReader("test"), closed: &closed}
	id := Insert(table, stream)

	// get entry and mark closed
	entry, err := Get(table, id)
	require.NoError(t, err)
	entry.closed.Store(true)

	// remove from table manually to simulate
	table.Remove(resource.Handle(id))

	// close via Close - entry already removed
	err = Close(table, id)
	assert.ErrorIs(t, err, streamapi.ErrNotFound)
}
