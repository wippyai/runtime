package stream

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	streamapi "github.com/wippyai/runtime/api/dispatcher/stream"
	"github.com/wippyai/runtime/api/resource"
)

func setupTestContext() (context.Context, *resource.Store) {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	store := resource.NewStore()
	_ = resource.SetStore(ctx, store)
	return ctx, store
}

func TestStreamRegistry(t *testing.T) {
	table := resource.NewTable()
	r := NewStreamRegistry(table)

	reader := io.NopCloser(strings.NewReader("hello"))
	id := r.Register(reader)
	if id != 1 {
		t.Errorf("expected first ID to be 1, got %d", id)
	}

	reader2 := io.NopCloser(strings.NewReader("world"))
	id2 := r.Register(reader2)
	if id2 != 2 {
		t.Errorf("expected second ID to be 2, got %d", id2)
	}
}

func TestStreamRegistryRead(t *testing.T) {
	table := resource.NewTable()
	r := NewStreamRegistry(table)

	data := "hello world"
	reader := io.NopCloser(strings.NewReader(data))
	id := r.Register(reader)

	chunk, err := r.Read(id, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(chunk) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(chunk))
	}

	chunk, err = r.Read(id, 6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(chunk) != " world" {
		t.Errorf("expected ' world', got '%s'", string(chunk))
	}

	// EOF
	chunk, err = r.Read(id, 10)
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestStreamRegistryReadNotFound(t *testing.T) {
	table := resource.NewTable()
	r := NewStreamRegistry(table)

	_, err := r.Read(999, 10)
	if err != ErrStreamNotFound {
		t.Errorf("expected ErrStreamNotFound, got %v", err)
	}
}

func TestStreamRegistryReadDefaultSize(t *testing.T) {
	table := resource.NewTable()
	r := NewStreamRegistry(table)

	data := strings.Repeat("x", 100)
	reader := io.NopCloser(strings.NewReader(data))
	id := r.Register(reader)

	// size=0 should use default
	chunk, err := r.Read(id, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunk) != 100 {
		t.Errorf("expected 100 bytes, got %d", len(chunk))
	}
}

func TestStreamRegistryClose(t *testing.T) {
	table := resource.NewTable()
	r := NewStreamRegistry(table)

	reader := io.NopCloser(strings.NewReader("test"))
	id := r.Register(reader)

	err := r.Close(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = r.Close(id)
	if err != ErrStreamNotFound {
		t.Errorf("expected ErrStreamNotFound on double close, got %v", err)
	}
}

func TestStreamRegistryTableClose(t *testing.T) {
	table := resource.NewTable()
	r := NewStreamRegistry(table)

	r.Register(io.NopCloser(strings.NewReader("a")))
	r.Register(io.NopCloser(strings.NewReader("b")))
	r.Register(io.NopCloser(strings.NewReader("c")))

	// Closing the table should clean up all streams
	table.Close()

	_, err := r.Read(1, 10)
	if err != ErrStreamNotFound {
		t.Errorf("expected ErrStreamNotFound after table close, got %v", err)
	}
}

func TestStreamReadHandler(t *testing.T) {
	ctx, _ := setupTestContext()
	registry := GetOrCreateStreamRegistry(ctx)

	data := "hello world"
	id := registry.Register(io.NopCloser(strings.NewReader(data)))

	h := NewStreamReadHandler()

	var emitted any
	err := h.Handle(ctx, streamapi.StreamReadCmd{StreamID: id, Size: 5}, func(d any) {
		emitted = d
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	chunk, ok := emitted.([]byte)
	if !ok {
		t.Fatalf("expected []byte, got %T", emitted)
	}
	if string(chunk) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(chunk))
	}
}

func TestStreamReadHandlerEOF(t *testing.T) {
	ctx, _ := setupTestContext()
	registry := GetOrCreateStreamRegistry(ctx)

	id := registry.Register(io.NopCloser(bytes.NewReader(nil)))

	h := NewStreamReadHandler()

	var emitted any
	err := h.Handle(ctx, streamapi.StreamReadCmd{StreamID: id, Size: 10}, func(d any) {
		emitted = d
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// nil signals EOF
	if emitted != nil {
		t.Errorf("expected nil for EOF, got %v", emitted)
	}
}

func TestStreamCloseHandler(t *testing.T) {
	ctx, _ := setupTestContext()
	registry := GetOrCreateStreamRegistry(ctx)

	id := registry.Register(io.NopCloser(strings.NewReader("test")))

	h := NewStreamCloseHandler()

	err := h.Handle(ctx, streamapi.StreamCloseCmd{StreamID: id}, func(d any) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = h.Handle(ctx, streamapi.StreamCloseCmd{StreamID: id}, func(d any) {})
	if err != ErrStreamNotFound {
		t.Errorf("expected ErrStreamNotFound on second close, got %v", err)
	}
}

func TestStreamFullCycle(t *testing.T) {
	ctx, _ := setupTestContext()
	registry := GetOrCreateStreamRegistry(ctx)

	data := "chunk1chunk2chunk3"
	id := registry.Register(io.NopCloser(strings.NewReader(data)))

	readH := NewStreamReadHandler()
	closeH := NewStreamCloseHandler()

	chunks := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		var emitted any
		err := readH.Handle(ctx, streamapi.StreamReadCmd{StreamID: id, Size: 6}, func(d any) {
			emitted = d
		})
		if err != nil {
			t.Fatalf("read %d error: %v", i, err)
		}
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

	err := closeH.Handle(ctx, streamapi.StreamCloseCmd{StreamID: id}, func(d any) {})
	if err != nil {
		t.Fatalf("close error: %v", err)
	}
}

func TestStreamService(t *testing.T) {
	s := NewService()
	if s.Read == nil || s.Close == nil || s.Write == nil || s.Seek == nil || s.Flush == nil || s.Stat == nil {
		t.Error("service handlers not initialized")
	}

	count := 0
	s.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		count++
	})
	if count != 6 {
		t.Errorf("expected 6 handlers registered, got %d", count)
	}
}

// trackingCloser records whether Close was called.
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

	registry := GetOrCreateStreamRegistry(ctx)
	registry.RegisterStream(&trackingCloser{strings.NewReader("a"), &closed1})
	registry.RegisterStream(&trackingCloser{strings.NewReader("b"), &closed2})
	registry.RegisterStream(&trackingCloser{strings.NewReader("c"), &closed3})

	// Verify streams are registered
	if _, err := registry.Read(1, 1); err != nil {
		t.Errorf("stream 1 should be readable: %v", err)
	}

	// Close store - should cleanup all streams via Table
	store.Close()

	// Verify all streams were closed via Dropper interface
	if !closed1 || !closed2 || !closed3 {
		t.Errorf("expected all streams closed, got: %v %v %v", closed1, closed2, closed3)
	}
}

func TestStreamCleanupIdempotent(t *testing.T) {
	ctx, store := setupTestContext()

	registry := GetOrCreateStreamRegistry(ctx)
	registry.RegisterStream(&trackingCloser{
		Reader: strings.NewReader("test"),
		closed: func() *bool {
			b := false
			return &b
		}(),
	})

	// Close store multiple times - should not panic
	store.Close()
	store.Close()
	store.Close()
}
