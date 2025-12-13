package stream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/runtime/resource"
	streamapi "github.com/wippyai/runtime/api/stream"
)

type testReceiver struct {
	fn func(data any)
}

func (r *testReceiver) CompleteYield(_ uint64, data any, _ error) {
	r.fn(data)
}

func setupTestContext() (context.Context, *resource.Store) {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	store := resource.NewStore()
	_ = resource.SetStore(ctx, store)
	return ctx, store
}

func TestStreamInsert(t *testing.T) {
	table := resource.NewTable()

	reader := io.NopCloser(strings.NewReader("hello"))
	id := Insert(table, reader)
	if id != 1 {
		t.Errorf("expected first ID to be 1, got %d", id)
	}

	reader2 := io.NopCloser(strings.NewReader("world"))
	id2 := Insert(table, reader2)
	if id2 != 2 {
		t.Errorf("expected second ID to be 2, got %d", id2)
	}
}

func TestStreamRead(t *testing.T) {
	table := resource.NewTable()

	data := "hello world"
	reader := io.NopCloser(strings.NewReader(data))
	id := Insert(table, reader)

	chunk, err := Read(table, id, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(chunk) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(chunk))
	}

	chunk, err = Read(table, id, 6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(chunk) != " world" {
		t.Errorf("expected ' world', got '%s'", string(chunk))
	}

	_, err = Read(table, id, 10)
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestStreamReadNotFound(t *testing.T) {
	table := resource.NewTable()

	_, err := Read(table, 999, 10)
	if !errors.Is(err, streamapi.ErrNotFound) {
		t.Errorf("expected streamapi.ErrNotFound, got %v", err)
	}
}

func TestStreamReadDefaultSize(t *testing.T) {
	table := resource.NewTable()

	data := strings.Repeat("x", 100)
	reader := io.NopCloser(strings.NewReader(data))
	id := Insert(table, reader)

	chunk, err := Read(table, id, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunk) != 100 {
		t.Errorf("expected 100 bytes, got %d", len(chunk))
	}
}

func TestStreamClose(t *testing.T) {
	table := resource.NewTable()

	reader := io.NopCloser(strings.NewReader("test"))
	id := Insert(table, reader)

	err := Close(table, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = Close(table, id)
	if !errors.Is(err, streamapi.ErrNotFound) {
		t.Errorf("expected streamapi.ErrNotFound on double close, got %v", err)
	}
}

func TestStreamTableClose(t *testing.T) {
	table := resource.NewTable()

	Insert(table, io.NopCloser(strings.NewReader("a")))
	Insert(table, io.NopCloser(strings.NewReader("b")))
	Insert(table, io.NopCloser(strings.NewReader("c")))

	table.Close()

	_, err := Read(table, 1, 10)
	if !errors.Is(err, streamapi.ErrNotFound) {
		t.Errorf("expected streamapi.ErrNotFound after table close, got %v", err)
	}
}

func TestStreamReadHandler(t *testing.T) {
	ctx, store := setupTestContext()
	defer store.Close()

	table := resource.GetTable(ctx)
	data := "hello world"
	id := Insert(table, io.NopCloser(strings.NewReader(data)))

	d := NewDispatcher()
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var emitted any
	done := make(chan struct{})
	err := handlers[streamapi.CmdRead].Handle(ctx, streamapi.ReadCmd{StreamID: id, Size: 5}, 0, &testReceiver{fn: func(d any) {
		emitted = d
		close(done)
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	<-done

	chunk, ok := emitted.([]byte)
	if !ok {
		t.Fatalf("expected []byte, got %T", emitted)
	}
	if string(chunk) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(chunk))
	}
}

func TestStreamReadHandlerEOF(t *testing.T) {
	ctx, store := setupTestContext()
	defer store.Close()

	table := resource.GetTable(ctx)
	id := Insert(table, io.NopCloser(bytes.NewReader(nil)))

	d := NewDispatcher()
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var emitted any
	done := make(chan struct{})
	err := handlers[streamapi.CmdRead].Handle(ctx, streamapi.ReadCmd{StreamID: id, Size: 10}, 0, &testReceiver{fn: func(d any) {
		emitted = d
		close(done)
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	<-done

	if emitted != nil {
		t.Errorf("expected nil for EOF, got %v", emitted)
	}
}

func TestStreamCloseHandler(t *testing.T) {
	ctx, store := setupTestContext()
	defer store.Close()

	table := resource.GetTable(ctx)
	id := Insert(table, io.NopCloser(strings.NewReader("test")))

	d := NewDispatcher()
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(cmdID dispatcher.CommandID, h dispatcher.Handler) {
		handlers[cmdID] = h
	})

	done := make(chan struct{})
	err := handlers[streamapi.CmdClose].Handle(ctx, streamapi.CloseCmd{StreamID: id}, 0, &testReceiver{fn: func(_ any) {
		close(done)
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	<-done

	// Second close should still complete but stream is already gone
	done2 := make(chan struct{})
	err = handlers[streamapi.CmdClose].Handle(ctx, streamapi.CloseCmd{StreamID: id}, 0, &testReceiver{fn: func(_ any) {
		close(done2)
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-done2:
	case <-time.After(100 * time.Millisecond):
		t.Error("expected completion for second close")
	}
}

func TestStreamFullCycle(t *testing.T) {
	ctx, store := setupTestContext()
	defer store.Close()

	table := resource.GetTable(ctx)
	data := "chunk1chunk2chunk3"
	id := Insert(table, io.NopCloser(strings.NewReader(data)))

	d := NewDispatcher()
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(cmdID dispatcher.CommandID, h dispatcher.Handler) {
		handlers[cmdID] = h
	})

	chunks := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		var emitted any
		done := make(chan struct{})
		err := handlers[streamapi.CmdRead].Handle(ctx, streamapi.ReadCmd{StreamID: id, Size: 6}, 0, &testReceiver{fn: func(d any) {
			emitted = d
			close(done)
		}})
		if err != nil {
			t.Fatalf("read %d error: %v", i, err)
		}
		<-done
		if chunk, ok := emitted.([]byte); ok {
			chunks = append(chunks, string(chunk))
		}
	}

	if len(chunks) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(chunks))
	}
	if chunks[0] != "chunk1" || chunks[1] != "chunk2" || chunks[2] != "chunk3" {
		t.Errorf("unexpected chunks: %v", chunks)
	}

	done := make(chan struct{})
	err := handlers[streamapi.CmdClose].Handle(ctx, streamapi.CloseCmd{StreamID: id}, 0, &testReceiver{fn: func(_ any) {
		close(done)
	}})
	if err != nil {
		t.Fatalf("close error: %v", err)
	}
	<-done
}

func TestDispatcher_RegisterAll(t *testing.T) {
	d := NewDispatcher()

	count := 0
	d.RegisterAll(func(_ dispatcher.CommandID, _ dispatcher.Handler) {
		count++
	})
	if count != 8 {
		t.Errorf("expected 8 handlers registered, got %d", count)
	}
}

func TestDispatcher_WithWorkers(t *testing.T) {
	d := NewDispatcher(WithWorkers(8))
	if d.workers != 8 {
		t.Errorf("expected 8 workers, got %d", d.workers)
	}

	d2 := NewDispatcher()
	if d2.workers != 16 {
		t.Errorf("expected default 16 workers, got %d", d2.workers)
	}
}

type trackingCloser struct {
	io.Reader
	closed *bool
}

func (tc *trackingCloser) Close() error {
	*tc.closed = true
	return nil
}

func TestStreamCleanupOnStoreClose(t *testing.T) {
	ctx, store := setupTestContext()

	var closed1, closed2, closed3 bool

	table := resource.GetTable(ctx)
	Insert(table, &trackingCloser{strings.NewReader("a"), &closed1})
	Insert(table, &trackingCloser{strings.NewReader("b"), &closed2})
	Insert(table, &trackingCloser{strings.NewReader("c"), &closed3})

	if _, err := Read(table, 1, 1); err != nil {
		t.Errorf("stream 1 should be readable: %v", err)
	}

	store.Close()

	if !closed1 || !closed2 || !closed3 {
		t.Errorf("expected all streams closed, got: %v %v %v", closed1, closed2, closed3)
	}
}

func TestStreamCleanupIdempotent(*testing.T) {
	ctx, store := setupTestContext()

	table := resource.GetTable(ctx)
	Insert(table, &trackingCloser{
		Reader: strings.NewReader("test"),
		closed: func() *bool {
			b := false
			return &b
		}(),
	})

	store.Close()
	store.Close()
	store.Close()
}

// rwsStream implements Reader, Writer, Seeker, Closer, Flusher, and Stater
type rwsStream struct {
	*bytes.Buffer
	closed    bool
	flushed   bool
	seekPos   int64
	statCalls int
}

func newRWSStream(data string) *rwsStream {
	return &rwsStream{Buffer: bytes.NewBufferString(data)}
}

func (s *rwsStream) Close() error {
	s.closed = true
	return nil
}

func (s *rwsStream) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		s.seekPos = offset
	case io.SeekCurrent:
		s.seekPos += offset
	case io.SeekEnd:
		s.seekPos = int64(s.Buffer.Len()) + offset
	}
	return s.seekPos, nil
}

func (s *rwsStream) Flush() error {
	s.flushed = true
	return nil
}

func (s *rwsStream) Stat() (int64, error) {
	s.statCalls++
	return int64(s.Buffer.Len()), nil
}

func TestEntry_Caps(t *testing.T) {
	table := resource.NewTable()
	stream := newRWSStream("test")
	id := Insert(table, stream)

	entry, err := Get(table, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	caps := entry.Caps()
	if !caps.Readable {
		t.Error("expected Readable to be true")
	}
	if !caps.Writable {
		t.Error("expected Writable to be true")
	}
	if !caps.Seekable {
		t.Error("expected Seekable to be true")
	}
}

func TestEntry_Reader(t *testing.T) {
	table := resource.NewTable()
	stream := newRWSStream("test")
	id := Insert(table, stream)

	entry, err := Get(table, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reader := entry.Reader()
	if reader == nil {
		t.Error("expected Reader to be non-nil")
	}
}

func TestEntry_Writer(t *testing.T) {
	table := resource.NewTable()
	stream := newRWSStream("")
	id := Insert(table, stream)

	entry, err := Get(table, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	writer := entry.Writer()
	if writer == nil {
		t.Error("expected Writer to be non-nil")
	}
}

func TestScannerEntry_Drop(t *testing.T) {
	entry := &ScannerEntry{}
	entry.Drop()
}

func TestStreamWrite(t *testing.T) {
	table := resource.NewTable()
	stream := newRWSStream("")
	id := Insert(table, stream)

	n, err := Write(table, id, []byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 bytes written, got %d", n)
	}
	if stream.String() != "hello" {
		t.Errorf("expected 'hello', got '%s'", stream.String())
	}
}

func TestStreamWriteNotFound(t *testing.T) {
	table := resource.NewTable()
	_, err := Write(table, 999, []byte("hello"))
	if !errors.Is(err, streamapi.ErrNotFound) {
		t.Errorf("expected streamapi.ErrNotFound, got %v", err)
	}
}

func TestStreamWriteNotWritable(t *testing.T) {
	table := resource.NewTable()
	reader := io.NopCloser(strings.NewReader("test"))
	id := Insert(table, reader)

	_, err := Write(table, id, []byte("hello"))
	if !errors.Is(err, streamapi.ErrNotWritable) {
		t.Errorf("expected streamapi.ErrNotWritable, got %v", err)
	}
}

func TestStreamSeek(t *testing.T) {
	table := resource.NewTable()
	stream := newRWSStream("test data")
	id := Insert(table, stream)

	pos, err := Seek(table, id, 5, io.SeekStart)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos != 5 {
		t.Errorf("expected position 5, got %d", pos)
	}
}

func TestStreamSeekNotFound(t *testing.T) {
	table := resource.NewTable()
	_, err := Seek(table, 999, 0, io.SeekStart)
	if !errors.Is(err, streamapi.ErrNotFound) {
		t.Errorf("expected streamapi.ErrNotFound, got %v", err)
	}
}

func TestStreamSeekNotSeekable(t *testing.T) {
	table := resource.NewTable()
	reader := io.NopCloser(strings.NewReader("test"))
	id := Insert(table, reader)

	_, err := Seek(table, id, 0, io.SeekStart)
	if !errors.Is(err, streamapi.ErrNotSeekable) {
		t.Errorf("expected streamapi.ErrNotSeekable, got %v", err)
	}
}

func TestStreamFlush(t *testing.T) {
	table := resource.NewTable()
	stream := newRWSStream("")
	id := Insert(table, stream)

	err := Flush(table, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stream.flushed {
		t.Error("expected stream to be flushed")
	}
}

func TestStreamFlushNotFound(t *testing.T) {
	table := resource.NewTable()
	err := Flush(table, 999)
	if !errors.Is(err, streamapi.ErrNotFound) {
		t.Errorf("expected streamapi.ErrNotFound, got %v", err)
	}
}

func TestStreamFlushNoFlusher(t *testing.T) {
	table := resource.NewTable()
	reader := io.NopCloser(strings.NewReader("test"))
	id := Insert(table, reader)

	err := Flush(table, id)
	if err != nil {
		t.Errorf("expected nil error for stream without Flusher, got %v", err)
	}
}

func TestStreamStat(t *testing.T) {
	table := resource.NewTable()
	stream := newRWSStream("test data")
	id := InsertWithSize(table, stream, -1)

	size, pos, caps, err := Stat(table, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if size != 9 {
		t.Errorf("expected size 9, got %d", size)
	}
	if pos != 0 {
		t.Errorf("expected position 0, got %d", pos)
	}
	if !caps.Readable || !caps.Writable || !caps.Seekable {
		t.Error("expected all capabilities to be true")
	}
}

func TestStreamStatWithKnownSize(t *testing.T) {
	table := resource.NewTable()
	stream := newRWSStream("test data")
	id := InsertWithSize(table, stream, 100)

	size, _, _, err := Stat(table, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if size != 100 {
		t.Errorf("expected size 100, got %d", size)
	}
}

func TestStreamStatNotFound(t *testing.T) {
	table := resource.NewTable()
	_, _, _, err := Stat(table, 999)
	if !errors.Is(err, streamapi.ErrNotFound) {
		t.Errorf("expected streamapi.ErrNotFound, got %v", err)
	}
}

func TestCreateScanner(t *testing.T) {
	table := resource.NewTable()
	data := "line1\nline2\nline3"
	reader := io.NopCloser(strings.NewReader(data))
	streamID := Insert(table, reader)

	scannerID, err := CreateScanner(table, streamID, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scannerID == 0 {
		t.Error("expected non-zero scanner ID")
	}
}

func TestCreateScannerNotFound(t *testing.T) {
	table := resource.NewTable()
	_, err := CreateScanner(table, 999, 0)
	if !errors.Is(err, streamapi.ErrNotFound) {
		t.Errorf("expected streamapi.ErrNotFound, got %v", err)
	}
}

func TestCreateScannerNotReadable(t *testing.T) {
	table := resource.NewTable()

	type writeOnlyCloser struct{ io.WriteCloser }
	stream := &writeOnlyCloser{WriteCloser: nopWriteCloser{bytes.NewBuffer(nil)}}
	id := Insert(table, stream)

	_, err := CreateScanner(table, id, 0)
	if !errors.Is(err, streamapi.ErrNotReadable) {
		t.Errorf("expected streamapi.ErrNotReadable, got %v", err)
	}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func TestGetScanner(t *testing.T) {
	table := resource.NewTable()
	data := "test"
	reader := io.NopCloser(strings.NewReader(data))
	streamID := Insert(table, reader)

	scannerID, err := CreateScanner(table, streamID, 0)
	if err != nil {
		t.Fatalf("unexpected error creating scanner: %v", err)
	}

	entry, err := GetScanner(table, scannerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry == nil {
		t.Error("expected non-nil scanner entry")
	}
}

func TestGetScannerNotFound(t *testing.T) {
	table := resource.NewTable()
	_, err := GetScanner(table, 999)
	if !errors.Is(err, streamapi.ErrScannerNotFound) {
		t.Errorf("expected streamapi.ErrScannerNotFound, got %v", err)
	}
}

func TestScanNext(t *testing.T) {
	table := resource.NewTable()
	data := "line1\nline2\nline3"
	reader := io.NopCloser(strings.NewReader(data))
	streamID := Insert(table, reader)

	scannerID, err := CreateScanner(table, streamID, 0)
	if err != nil {
		t.Fatalf("unexpected error creating scanner: %v", err)
	}

	result, err := ScanNext(table, scannerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasToken {
		t.Error("expected HasToken to be true")
	}
	if result.Text != "line1" {
		t.Errorf("expected 'line1', got '%s'", result.Text)
	}

	result, err = ScanNext(table, scannerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "line2" {
		t.Errorf("expected 'line2', got '%s'", result.Text)
	}

	result, err = ScanNext(table, scannerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "line3" {
		t.Errorf("expected 'line3', got '%s'", result.Text)
	}

	result, err = ScanNext(table, scannerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasToken {
		t.Error("expected HasToken to be false at EOF")
	}

	result, err = ScanNext(table, scannerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasToken {
		t.Error("expected HasToken to remain false after EOF")
	}
}

func TestScanNextNotFound(t *testing.T) {
	table := resource.NewTable()
	_, err := ScanNext(table, 999)
	if !errors.Is(err, streamapi.ErrScannerNotFound) {
		t.Errorf("expected streamapi.ErrScannerNotFound, got %v", err)
	}
}

func TestErrString(t *testing.T) {
	if errString(nil) != "" {
		t.Error("expected empty string for nil error")
	}
	if errString(errors.New("test")) != "test" {
		t.Error("expected 'test' for non-nil error")
	}
}

func TestWithDebug(t *testing.T) {
	var buf bytes.Buffer
	d := NewDispatcher(WithDebug(&buf))
	if d.debug == nil {
		t.Error("expected debug writer to be set")
	}

	ctx, store := setupTestContext()
	defer store.Close()

	table := resource.GetTable(ctx)
	id := Insert(table, io.NopCloser(strings.NewReader("test")))

	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(cmdID dispatcher.CommandID, h dispatcher.Handler) {
		handlers[cmdID] = h
	})

	done := make(chan struct{})
	_ = handlers[streamapi.CmdRead].Handle(ctx, streamapi.ReadCmd{StreamID: id, Size: 4}, 0, &testReceiver{fn: func(_ any) {
		close(done)
	}})
	<-done

	if buf.Len() == 0 {
		t.Error("expected debug output")
	}
}

func TestGetWrongType(t *testing.T) {
	table := resource.NewTable()
	table.Insert(TypeScanner, &ScannerEntry{})

	_, err := Get(table, 1)
	if err == nil {
		t.Error("expected error for wrong type")
	}
	if !strings.Contains(err.Error(), "wrong type") {
		t.Errorf("expected 'wrong type' in error, got: %v", err)
	}
}

func TestGetClosed(t *testing.T) {
	table := resource.NewTable()
	reader := io.NopCloser(strings.NewReader("test"))
	id := Insert(table, reader)

	_ = Close(table, id)

	entry, ok := table.GetTyped(resource.Handle(id), TypeStream)
	if ok {
		t.Error("expected entry to be removed after close")
	}
	if entry != nil {
		t.Error("expected nil entry after close")
	}
}

func BenchmarkStreamInsert(b *testing.B) {
	table := resource.NewTable()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		reader := io.NopCloser(strings.NewReader("benchmark data"))
		Insert(table, reader)
	}
}

func BenchmarkStreamRead(b *testing.B) {
	table := resource.NewTable()
	data := strings.Repeat("x", 1024)
	ids := make([]uint64, 100)
	for i := range ids {
		ids[i] = Insert(table, io.NopCloser(strings.NewReader(data)))
	}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		id := ids[i%len(ids)]
		_, _ = Read(table, id, 64)
	}
}

func BenchmarkStreamInsertClose(b *testing.B) {
	table := resource.NewTable()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		reader := io.NopCloser(strings.NewReader("benchmark data"))
		id := Insert(table, reader)
		_ = Close(table, id)
	}
}

func BenchmarkStreamWrite(b *testing.B) {
	table := resource.NewTable()
	buf := &bytes.Buffer{}
	id := Insert(table, struct {
		io.Reader
		io.Writer
		io.Closer
	}{
		Reader: buf,
		Writer: buf,
		Closer: io.NopCloser(nil),
	})
	data := []byte("benchmark write data")
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf.Reset()
		_, _ = Write(table, id, data)
	}
}
