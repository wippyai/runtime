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
)

func TestStreamRegistry(t *testing.T) {
	r := NewStreamRegistry()

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
	r := NewStreamRegistry()

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
	r := NewStreamRegistry()

	_, err := r.Read(999, 10)
	if err != ErrStreamNotFound {
		t.Errorf("expected ErrStreamNotFound, got %v", err)
	}
}

func TestStreamRegistryReadDefaultSize(t *testing.T) {
	r := NewStreamRegistry()

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
	r := NewStreamRegistry()

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

func TestStreamRegistryCloseAll(t *testing.T) {
	r := NewStreamRegistry()

	r.Register(io.NopCloser(strings.NewReader("a")))
	r.Register(io.NopCloser(strings.NewReader("b")))
	r.Register(io.NopCloser(strings.NewReader("c")))

	r.CloseAll()

	_, err := r.Read(1, 10)
	if err != ErrStreamNotFound {
		t.Errorf("expected ErrStreamNotFound after CloseAll, got %v", err)
	}
}

func TestStreamReadHandler(t *testing.T) {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
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
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
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
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
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
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
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
	if s.Read == nil || s.Close == nil {
		t.Error("service handlers not initialized")
	}

	count := 0
	s.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		count++
	})
	if count != 2 {
		t.Errorf("expected 2 handlers registered, got %d", count)
	}
}
