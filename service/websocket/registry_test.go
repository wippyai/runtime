// SPDX-License-Identifier: MPL-2.0

package websocket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/runtime/resource"
	wsapi "github.com/wippyai/runtime/api/service/websocket"

	"github.com/coder/websocket"
)

func TestRegistryNew(t *testing.T) {
	table := resource.NewTable()
	defer func() { _ = table.Close() }()

	r := NewRegistry(table, nil)
	if r == nil {
		t.Fatal("expected non-nil registry")
		return
	}
	if r.conns == nil {
		t.Error("expected typed table")
	}
}

func TestRegistryGetMessageChanNotFound(t *testing.T) {
	table := resource.NewTable()
	defer func() { _ = table.Close() }()
	r := NewRegistry(table, nil)

	_, err := r.GetMessageChan(999)
	if !errors.Is(err, ErrConnNotFound) {
		t.Errorf("expected ErrConnNotFound, got %v", err)
	}
}

func TestRegistryCloseNotFound(t *testing.T) {
	table := resource.NewTable()
	defer func() { _ = table.Close() }()
	r := NewRegistry(table, nil)

	err := r.Close(999, 0, "")
	if !errors.Is(err, ErrConnNotFound) {
		t.Errorf("expected ErrConnNotFound, got %v", err)
	}
}

func TestRegistryRegisterAndGetMessageChan(t *testing.T) {
	ts := newReadOnlyServer()
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	conn, resp, err := websocket.Dial(context.Background(), wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	ctx, store := newTestContext()
	defer func() { _ = store.Close() }()

	r := NewRegistry(store.Table(), nil)
	id := r.Register(ctx, conn, 16, 0)

	if id == 0 {
		t.Error("expected non-zero connection ID")
	}

	msgCh, err := r.GetMessageChan(id)
	if err != nil {
		t.Fatalf("GetMessageChan failed: %v", err)
	}
	if msgCh == nil {
		t.Error("expected non-nil message channel")
	}
}

func TestRegistryClose(t *testing.T) {
	ts := newReadOnlyServer()
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	conn, resp, err := websocket.Dial(context.Background(), wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	ctx, store := newTestContext()
	defer func() { _ = store.Close() }()

	r := NewRegistry(store.Table(), nil)
	id := r.Register(ctx, conn, 16, 0)

	if err := r.Close(id, 1000, "test"); err != nil {
		t.Errorf("close failed: %v", err)
	}

	_, err = r.GetMessageChan(id)
	if !errors.Is(err, ErrConnNotFound) {
		t.Errorf("expected ErrConnNotFound after close, got %v", err)
	}
}

func TestRegistryDoubleClose(t *testing.T) {
	ts := newReadOnlyServer()
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	conn, resp, err := websocket.Dial(context.Background(), wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	ctx, store := newTestContext()
	defer func() { _ = store.Close() }()

	r := NewRegistry(store.Table(), nil)
	id := r.Register(ctx, conn, 16, 0)

	if err := r.Close(id, 1000, "first"); err != nil {
		t.Errorf("first close failed: %v", err)
	}

	err = r.Close(id, 1000, "second")
	if !errors.Is(err, ErrConnNotFound) {
		t.Errorf("expected ErrConnNotFound on second close, got %v", err)
	}
}

func TestRegistryResourceCleanup(t *testing.T) {
	ts := newReadOnlyServer()
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := newTestContext()
	r := NewRegistry(store.Table(), nil)

	const numConns = 5
	for i := 0; i < numConns; i++ {
		conn, resp, err := websocket.Dial(context.Background(), wsURL, nil)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			t.Fatalf("dial %d failed: %v", i, err)
		}
		r.Register(ctx, conn, 16, 0)
	}

	if store.Table().Len() != numConns {
		t.Errorf("expected %d connections, got %d", numConns, store.Table().Len())
	}

	_ = store.Close()

	if store.Table().Len() != 0 {
		t.Errorf("expected 0 connections after Close, got %d", store.Table().Len())
	}
}

func TestRegistryReadLoop(t *testing.T) {
	const numMessages = 3
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		for i := 0; i < numMessages; i++ {
			_ = conn.Write(r.Context(), websocket.MessageText, []byte("msg"))
		}
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := newTestContext()
	defer func() { _ = store.Close() }()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	r := NewRegistry(store.Table(), nil)
	id := r.Register(ctx, conn, 16, 0)

	msgCh, _ := r.GetMessageChan(id)

	var received int
	for msg := range msgCh {
		if msg.EOF {
			break
		}
		received++
	}

	if received != numMessages {
		t.Errorf("expected %d messages, got %d", numMessages, received)
	}
}

func TestRegistryReadLoopBinary(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		_ = conn.Write(r.Context(), websocket.MessageBinary, []byte{0x01, 0x02})
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := newTestContext()
	defer func() { _ = store.Close() }()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	r := NewRegistry(store.Table(), nil)
	id := r.Register(ctx, conn, 16, 0)

	msgCh, _ := r.GetMessageChan(id)
	msg := <-msgCh

	if msg.MessageType != wsapi.MessageBinary {
		t.Errorf("expected binary, got %d", msg.MessageType)
	}
}

func TestRegistryContextCancel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping context cancel test in short mode")
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		time.Sleep(10 * time.Second)
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	_, store := newTestContext()
	defer func() { _ = store.Close() }()

	ctx, cancel := context.WithCancel(context.Background())

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	r := NewRegistry(store.Table(), nil)
	id := r.Register(ctx, conn, 16, 0)

	msgCh, _ := r.GetMessageChan(id)

	cancel()

	select {
	case _, ok := <-msgCh:
		_ = ok // Either got EOF message or channel closed, both are fine
	case <-time.After(time.Second):
		t.Error("channel should close on context cancel")
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	ts := newReadOnlyServer()
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := newTestContext()
	defer func() { _ = store.Close() }()

	r := NewRegistry(store.Table(), nil)

	var wg sync.WaitGroup
	const numGoroutines = 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, resp, err := websocket.Dial(context.Background(), wsURL, nil)
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			if err != nil {
				return
			}

			id := r.Register(ctx, conn, 16, 0)
			_, _ = r.GetMessageChan(id)
			_ = r.Close(id, 1000, "test")
		}()
	}

	assert.NotPanics(t, func() {
		wg.Wait()
	})
}

func TestGetRegistry(t *testing.T) {
	ctx, store := newTestContext()
	defer func() { _ = store.Close() }()

	r1 := GetRegistry(ctx)
	if r1 == nil {
		t.Fatal("expected registry")
	}

	r2 := GetRegistry(ctx)
	if r1 != r2 {
		t.Error("expected same registry instance (cached)")
	}
}

func TestGetRegistryNoTable(t *testing.T) {
	r := GetRegistry(context.Background())
	if r != nil {
		t.Error("expected nil registry without table")
	}
}

func TestMustGetRegistryPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()

	MustGetRegistry(context.Background())
}

func TestConnEntryDrop(t *testing.T) {
	ts := newReadOnlyServer()
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := newTestContext()

	conn, resp, err := websocket.Dial(context.Background(), wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	r := NewRegistry(store.Table(), nil)
	id := r.Register(ctx, conn, 16, 0)

	msgCh, _ := r.GetMessageChan(id)

	// Closing store triggers Drop on all entries
	_ = store.Close()

	select {
	case _, ok := <-msgCh:
		_ = ok // May get EOF or channel closed, both are fine
	case <-time.After(time.Second):
		t.Error("channel should close on Drop")
	}
}

// Helpers

func newTestContext() (context.Context, *resource.Store) {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	store := resource.NewStore()
	_ = resource.SetStore(ctx, store)
	return ctx, store
}

func newReadOnlyServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		for {
			_, _, err := conn.Read(r.Context())
			if err != nil {
				return
			}
		}
	}))
}

// Benchmarks

func BenchmarkRegistryRegister(b *testing.B) {
	ts := newReadOnlyServer()
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := newTestContext()
	defer func() { _ = store.Close() }()

	r := NewRegistry(store.Table(), nil)

	// Pre-dial connections
	conns := make([]*websocket.Conn, b.N)
	for i := 0; i < b.N; i++ {
		conn, resp, err := websocket.Dial(context.Background(), wsURL, nil)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			b.Fatalf("dial failed: %v", err)
		}
		conns[i] = conn
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Register(ctx, conns[i], 16, 0)
	}
}

func BenchmarkRegistryGetMessageChan(b *testing.B) {
	ts := newReadOnlyServer()
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := newTestContext()
	defer func() { _ = store.Close() }()

	r := NewRegistry(store.Table(), nil)

	conn, resp, err := websocket.Dial(context.Background(), wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		b.Fatalf("dial failed: %v", err)
	}

	id := r.Register(ctx, conn, 16, 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.GetMessageChan(id)
	}
}

func BenchmarkRegistryClose(b *testing.B) {
	ts := newReadOnlyServer()
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := newTestContext()
	defer func() { _ = store.Close() }()

	r := NewRegistry(store.Table(), nil)

	// Pre-register connections
	ids := make([]uint64, b.N)
	for i := 0; i < b.N; i++ {
		conn, resp, err := websocket.Dial(context.Background(), wsURL, nil)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			b.Fatalf("dial failed: %v", err)
		}
		ids[i] = r.Register(ctx, conn, 16, 0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Close(ids[i], 1000, "bench")
	}
}

func BenchmarkRegistryConcurrentGet(b *testing.B) {
	ts := newReadOnlyServer()
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := newTestContext()
	defer func() { _ = store.Close() }()

	r := NewRegistry(store.Table(), nil)

	conn, resp, err := websocket.Dial(context.Background(), wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		b.Fatalf("dial failed: %v", err)
	}

	id := r.Register(ctx, conn, 16, 0)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = r.GetMessageChan(id)
		}
	})
}
