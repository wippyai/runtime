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

	chunk, err = Read(table, id, 10)
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestStreamReadNotFound(t *testing.T) {
	table := resource.NewTable()

	_, err := Read(table, 999, 10)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
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
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound on double close, got %v", err)
	}
}

func TestStreamTableClose(t *testing.T) {
	table := resource.NewTable()

	Insert(table, io.NopCloser(strings.NewReader("a")))
	Insert(table, io.NopCloser(strings.NewReader("b")))
	Insert(table, io.NopCloser(strings.NewReader("c")))

	table.Close()

	_, err := Read(table, 1, 10)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after table close, got %v", err)
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
	if count != 6 {
		t.Errorf("expected 6 handlers registered, got %d", count)
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
