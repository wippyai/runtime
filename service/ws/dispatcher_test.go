package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	wsapi "github.com/wippyai/runtime/api/dispatcher/ws"
	"github.com/wippyai/runtime/api/resource"

	"github.com/coder/websocket"
)

// setupTestContext creates a FrameContext with a resource Store
func setupTestContext() (context.Context, ctxapi.FrameContext, *resource.Store) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	store := resource.NewStore()
	_ = resource.SetStore(ctx, store)
	return ctx, fc, store
}

func TestWsRegistry(t *testing.T) {
	table := resource.NewTable()
	defer table.Close()
	r := NewWsRegistry(table)
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
}

func TestWsRegistryGetNotFound(t *testing.T) {
	table := resource.NewTable()
	defer table.Close()
	r := NewWsRegistry(table)

	_, err := r.Get(999)
	if err != ErrConnNotFound {
		t.Errorf("expected ErrConnNotFound, got %v", err)
	}
}

func TestWsRegistryCloseNotFound(t *testing.T) {
	table := resource.NewTable()
	defer table.Close()
	r := NewWsRegistry(table)

	err := r.Close(999, 0, "")
	if err != ErrConnNotFound {
		t.Errorf("expected ErrConnNotFound, got %v", err)
	}
}

func TestWsRegistryRegisterAndGet(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("accept error: %v", err)
			return
		}
		defer conn.CloseNow()

		for {
			_, _, err := conn.Read(context.Background())
			if err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.CloseNow()

	table := resource.NewTable()
	defer table.Close()
	r := NewWsRegistry(table)
	id := r.Register(conn)

	if id == 0 {
		t.Error("expected non-zero connection ID")
	}

	entry, err := r.Get(id)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if entry.conn != conn {
		t.Error("connection mismatch")
	}
}

func TestWsRegistryClose(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(context.Background())
			if err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	table := resource.NewTable()
	defer table.Close()
	r := NewWsRegistry(table)
	id := r.Register(conn)

	err = r.Close(id, 1000, "test close")
	if err != nil {
		t.Errorf("close failed: %v", err)
	}

	_, err = r.Get(id)
	if err != ErrConnNotFound {
		t.Errorf("expected ErrConnNotFound after close, got %v", err)
	}
}

func TestWsRegistryCloseAll(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(context.Background())
			if err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	table := resource.NewTable()
	r := NewWsRegistry(table)

	for i := 0; i < 5; i++ {
		conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
		if err != nil {
			t.Fatalf("dial %d failed: %v", i, err)
		}
		r.Register(conn)
	}

	// Table Close calls Drop() on all resources
	table.Close()

	if table.Len() != 0 {
		t.Errorf("expected 0 connections after Close, got %d", table.Len())
	}
}

func TestWsRegistryDoubleClose(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(context.Background())
			if err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	table := resource.NewTable()
	defer table.Close()
	r := NewWsRegistry(table)
	id := r.Register(conn)

	err = r.Close(id, 1000, "first close")
	if err != nil {
		t.Errorf("first close failed: %v", err)
	}

	err = r.Close(id, 1000, "second close")
	if err != ErrConnNotFound {
		t.Errorf("expected ErrConnNotFound on second close, got %v", err)
	}
}

func TestWsConnectHandler(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(context.Background())
			if err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]

	ctx, _, store := setupTestContext()
	defer store.Close()
	h := NewWsConnectHandler()

	var connID uint64
	err := h.Handle(ctx, wsapi.WsConnectCmd{URL: wsURL}, func(data any) {
		connID = data.(uint64)
	})

	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if connID == 0 {
		t.Error("expected non-zero connection ID")
	}

	registry := GetWsRegistry(ctx)
	if registry == nil {
		t.Fatal("expected registry in context")
	}

	entry, err := registry.Get(connID)
	if err != nil {
		t.Fatalf("get connection failed: %v", err)
	}
	if entry == nil {
		t.Error("expected connection entry")
	}
}

func TestWsConnectHandlerWithHeaders(t *testing.T) {
	var receivedHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(context.Background())
			if err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, _, store := setupTestContext()
	defer store.Close()
	h := NewWsConnectHandler()

	err := h.Handle(ctx, wsapi.WsConnectCmd{
		URL:     wsURL,
		Headers: map[string]string{"X-Custom": "test-value"},
	}, func(data any) {})

	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	if receivedHeaders.Get("X-Custom") != "test-value" {
		t.Errorf("expected X-Custom header, got %v", receivedHeaders)
	}
}

func TestWsSendHandler(t *testing.T) {
	var receivedData []byte
	var wg sync.WaitGroup
	wg.Add(1)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		_, data, err := conn.Read(context.Background())
		if err == nil {
			receivedData = data
		}
		wg.Done()
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, _, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateWsRegistry(ctx)
	connID := registry.Register(conn)

	h := NewWsSendHandler()
	err = h.Handle(ctx, wsapi.WsSendCmd{
		ConnID:      connID,
		Data:        []byte("hello"),
		MessageType: wsapi.MessageText,
	}, func(data any) {})

	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	wg.Wait()
	if string(receivedData) != "hello" {
		t.Errorf("expected 'hello', got '%s'", receivedData)
	}
}

func TestWsReceiveHandler(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		conn.Write(context.Background(), websocket.MessageText, []byte("server message"))
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, _, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateWsRegistry(ctx)
	connID := registry.Register(conn)

	h := NewWsReceiveHandler()
	var msg wsapi.WsMessage
	err = h.Handle(ctx, wsapi.WsReceiveCmd{ConnID: connID}, func(data any) {
		msg = data.(wsapi.WsMessage)
	})

	if err != nil {
		t.Fatalf("receive failed: %v", err)
	}
	if string(msg.Data) != "server message" {
		t.Errorf("expected 'server message', got '%s'", msg.Data)
	}
	if msg.MessageType != wsapi.MessageText {
		t.Errorf("expected text message type, got %d", msg.MessageType)
	}
	if msg.EOF {
		t.Error("expected EOF=false")
	}
}

func TestWsReceiveHandlerBinary(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		conn.Write(context.Background(), websocket.MessageBinary, []byte{0x00, 0x01, 0x02})
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, _, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateWsRegistry(ctx)
	connID := registry.Register(conn)

	h := NewWsReceiveHandler()
	var msg wsapi.WsMessage
	err = h.Handle(ctx, wsapi.WsReceiveCmd{ConnID: connID}, func(data any) {
		msg = data.(wsapi.WsMessage)
	})

	if err != nil {
		t.Fatalf("receive failed: %v", err)
	}
	if msg.MessageType != wsapi.MessageBinary {
		t.Errorf("expected binary message type, got %d", msg.MessageType)
	}
}

func TestWsReceiveHandlerEOF(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn.Close(websocket.StatusNormalClosure, "closing")
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, _, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateWsRegistry(ctx)
	connID := registry.Register(conn)

	h := NewWsReceiveHandler()
	var msg wsapi.WsMessage
	err = h.Handle(ctx, wsapi.WsReceiveCmd{ConnID: connID}, func(data any) {
		msg = data.(wsapi.WsMessage)
	})

	if err != nil {
		t.Fatalf("receive failed: %v", err)
	}
	if !msg.EOF {
		t.Error("expected EOF=true on server close")
	}
}

func TestWsCloseHandler(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(context.Background())
			if err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, _, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateWsRegistry(ctx)
	connID := registry.Register(conn)

	h := NewWsCloseHandler()
	err = h.Handle(ctx, wsapi.WsCloseCmd{
		ConnID: connID,
		Code:   1000,
		Reason: "normal close",
	}, func(data any) {})

	if err != nil {
		t.Fatalf("close failed: %v", err)
	}

	_, err = registry.Get(connID)
	if err != ErrConnNotFound {
		t.Errorf("expected ErrConnNotFound after close, got %v", err)
	}
}

func TestWsPingHandler(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(context.Background())
			if err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	baseCtx, _, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(baseCtx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateWsRegistry(baseCtx)
	connID := registry.Register(conn)

	ctx, cancel := context.WithTimeout(baseCtx, 2*time.Second)
	defer cancel()

	// Ping requires concurrent Read to receive the pong response
	go func() {
		for {
			_, _, err := conn.Read(ctx)
			if err != nil {
				return
			}
		}
	}()

	h := NewWsPingHandler()
	err = h.Handle(ctx, wsapi.WsPingCmd{ConnID: connID}, func(data any) {})

	if err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}

func TestWsSendHandlerNotFound(t *testing.T) {
	ctx, _, store := setupTestContext()
	defer store.Close()
	GetOrCreateWsRegistry(ctx)

	h := NewWsSendHandler()
	err := h.Handle(ctx, wsapi.WsSendCmd{
		ConnID: 99999,
		Data:   []byte("test"),
	}, func(data any) {})

	if err != ErrConnNotFound {
		t.Errorf("expected ErrConnNotFound, got %v", err)
	}
}

func TestWsReceiveHandlerNotFound(t *testing.T) {
	ctx, _, store := setupTestContext()
	defer store.Close()
	GetOrCreateWsRegistry(ctx)

	h := NewWsReceiveHandler()
	err := h.Handle(ctx, wsapi.WsReceiveCmd{ConnID: 99999}, func(data any) {})

	if err != ErrConnNotFound {
		t.Errorf("expected ErrConnNotFound, got %v", err)
	}
}

func TestWsConcurrentSends(t *testing.T) {
	var msgCount atomic.Int64
	var wg sync.WaitGroup
	const numMessages = 50

	wg.Add(1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		for {
			_, _, err := conn.Read(context.Background())
			if err != nil {
				break
			}
			msgCount.Add(1)
			if msgCount.Load() >= numMessages {
				wg.Done()
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, _, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateWsRegistry(ctx)
	connID := registry.Register(conn)

	h := NewWsSendHandler()

	var sendWg sync.WaitGroup
	for i := 0; i < numMessages; i++ {
		sendWg.Add(1)
		go func(i int) {
			defer sendWg.Done()
			h.Handle(ctx, wsapi.WsSendCmd{
				ConnID:      connID,
				Data:        []byte("msg"),
				MessageType: wsapi.MessageText,
			}, func(data any) {})
		}(i)
	}

	sendWg.Wait()
	wg.Wait()

	if msgCount.Load() != numMessages {
		t.Errorf("expected %d messages, got %d", numMessages, msgCount.Load())
	}
}

func TestWsFullCycle(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		for {
			msgType, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			conn.Write(context.Background(), msgType, append([]byte("echo:"), data...))
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, _, store := setupTestContext()
	defer store.Close()

	connectH := NewWsConnectHandler()
	sendH := NewWsSendHandler()
	receiveH := NewWsReceiveHandler()
	closeH := NewWsCloseHandler()

	var connID uint64
	err := connectH.Handle(ctx, wsapi.WsConnectCmd{URL: wsURL}, func(data any) {
		connID = data.(uint64)
	})
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	err = sendH.Handle(ctx, wsapi.WsSendCmd{
		ConnID:      connID,
		Data:        []byte("hello"),
		MessageType: wsapi.MessageText,
	}, func(data any) {})
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	var msg wsapi.WsMessage
	err = receiveH.Handle(ctx, wsapi.WsReceiveCmd{ConnID: connID}, func(data any) {
		msg = data.(wsapi.WsMessage)
	})
	if err != nil {
		t.Fatalf("receive failed: %v", err)
	}
	if string(msg.Data) != "echo:hello" {
		t.Errorf("expected 'echo:hello', got '%s'", msg.Data)
	}

	err = closeH.Handle(ctx, wsapi.WsCloseCmd{
		ConnID: connID,
		Code:   1000,
		Reason: "done",
	}, func(data any) {})
	if err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestWsContextCancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		time.Sleep(5 * time.Second)
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	baseCtx, _, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(baseCtx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateWsRegistry(baseCtx)
	connID := registry.Register(conn)

	ctx, cancel := context.WithTimeout(baseCtx, 50*time.Millisecond)
	defer cancel()

	h := NewWsReceiveHandler()
	start := time.Now()
	err = h.Handle(ctx, wsapi.WsReceiveCmd{ConnID: connID}, func(data any) {})
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected error on context cancellation")
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("cancellation took too long: %v", elapsed)
	}
}

func TestWsService(t *testing.T) {
	s := NewService()
	if s.Connect == nil || s.Send == nil || s.Receive == nil || s.Close == nil || s.Ping == nil || s.Subscribe == nil {
		t.Error("service handlers not initialized")
	}

	handlers := make(map[dispatcher.CommandID]bool)
	s.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = true
	})

	expected := []dispatcher.CommandID{
		wsapi.CmdWsConnect,
		wsapi.CmdWsSend,
		wsapi.CmdWsReceive,
		wsapi.CmdWsClose,
		wsapi.CmdWsPing,
		wsapi.CmdWsSubscribe,
	}

	for _, id := range expected {
		if !handlers[id] {
			t.Errorf("handler for command %d not registered", id)
		}
	}
}

func TestWsSubscribeHandler(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		for i := 0; i < 3; i++ {
			conn.Write(context.Background(), websocket.MessageText, []byte("msg"))
			time.Sleep(10 * time.Millisecond)
		}
		conn.Close(websocket.StatusNormalClosure, "done")
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, _, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateWsRegistry(ctx)
	connID := registry.Register(conn)

	var received []wsapi.WsMessage
	var mu sync.Mutex
	done := make(chan struct{})

	h := NewWsSubscribeHandler()
	err = h.Handle(ctx, wsapi.WsSubscribeCmd{ConnID: connID}, func(data any) {
		mu.Lock()
		defer mu.Unlock()
		switch v := data.(type) {
		case wsapi.WsSubscription:
			// Initial subscription confirmation
		case wsapi.WsMessage:
			received = append(received, v)
			if v.EOF {
				close(done)
			}
		}
	})

	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for messages")
	}

	mu.Lock()
	defer mu.Unlock()

	// Should have 3 messages + 1 EOF
	if len(received) != 4 {
		t.Errorf("expected 4 messages (3 data + EOF), got %d", len(received))
	}

	// Last should be EOF
	if len(received) > 0 && !received[len(received)-1].EOF {
		t.Error("last message should be EOF")
	}
}

func TestWsCleanupOnFrameClose(t *testing.T) {
	var serverClosed atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(context.Background())
			if err != nil {
				serverClosed.Store(true)
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, _, store := setupTestContext()

	// Connect via handler
	h := NewWsConnectHandler()
	var connID uint64
	err := h.Handle(ctx, wsapi.WsConnectCmd{URL: wsURL}, func(data any) {
		connID = data.(uint64)
	})
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	// Verify connection is active
	registry := GetWsRegistry(ctx)
	if registry == nil {
		t.Fatal("registry should exist")
	}
	_, err = registry.Get(connID)
	if err != nil {
		t.Fatalf("connection should be active: %v", err)
	}

	// Close the store (simulates process eviction)
	store.Close()

	// Wait for server to detect close
	time.Sleep(50 * time.Millisecond)

	// Verify cleanup happened
	if !serverClosed.Load() {
		t.Error("server should have detected connection close")
	}

	// Table should be empty after cleanup
	if store.Table().Len() != 0 {
		t.Errorf("expected 0 connections after cleanup, got %d", store.Table().Len())
	}
}

func TestWsCleanupMultipleConnections(t *testing.T) {
	var closeCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(context.Background())
			if err != nil {
				closeCount.Add(1)
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, _, store := setupTestContext()

	h := NewWsConnectHandler()

	// Open 5 connections
	for i := 0; i < 5; i++ {
		err := h.Handle(ctx, wsapi.WsConnectCmd{URL: wsURL}, func(data any) {})
		if err != nil {
			t.Fatalf("connect %d failed: %v", i, err)
		}
	}

	if store.Table().Len() != 5 {
		t.Errorf("expected 5 connections, got %d", store.Table().Len())
	}

	// Close store - should cleanup all connections
	store.Close()

	time.Sleep(100 * time.Millisecond)

	if closeCount.Load() != 5 {
		t.Errorf("expected 5 connections closed, got %d", closeCount.Load())
	}
}

func BenchmarkWsSend(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(context.Background())
			if err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, _, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		b.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateWsRegistry(ctx)
	connID := registry.Register(conn)
	h := NewWsSendHandler()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			h.Handle(ctx, wsapi.WsSendCmd{
				ConnID:      connID,
				Data:        []byte("benchmark"),
				MessageType: wsapi.MessageText,
			}, func(data any) {})
		}
	})
}
