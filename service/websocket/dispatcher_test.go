package websocket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/runtime/resource"
	wsapi "github.com/wippyai/runtime/api/websocket"

	"github.com/coder/websocket"
)

// testReceiver implements process.ResultReceiver for tests.
type testReceiver struct {
	fn func(tag uint64, data any, err error)
}

func (r *testReceiver) CompleteYield(tag uint64, data any, err error) {
	if r.fn != nil {
		r.fn(tag, data, err)
	}
}

// setupTestContext creates a FrameContext with a resource Store
func setupTestContext() (context.Context, *resource.Store) {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	store := resource.NewStore()
	_ = resource.SetStore(ctx, store)
	return ctx, store
}

// echoServer creates a test server that echoes messages back
func echoServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("accept error: %v", err)
			return
		}
		defer conn.CloseNow()

		for {
			msgType, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			if err := conn.Write(r.Context(), msgType, data); err != nil {
				return
			}
		}
	}))
}

// readOnlyServer creates a server that just reads messages
func readOnlyServer(*testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(r.Context())
			if err != nil {
				return
			}
		}
	}))
}

func TestRegistry(t *testing.T) {
	table := resource.NewTable()
	defer table.Close()
	r := NewRegistry(table)
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	table := resource.NewTable()
	defer table.Close()
	r := NewRegistry(table)

	_, err := r.Get(999)
	if !errors.Is(err, ErrConnNotFound) {
		t.Errorf("expected ErrConnNotFound, got %v", err)
	}
}

func TestRegistryCloseNotFound(t *testing.T) {
	table := resource.NewTable()
	defer table.Close()
	r := NewRegistry(table)

	err := r.Close(999, 0, "")
	if !errors.Is(err, ErrConnNotFound) {
		t.Errorf("expected ErrConnNotFound, got %v", err)
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	ts := readOnlyServer(t)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.CloseNow()

	ctx, store := setupTestContext()
	defer store.Close()

	r := NewRegistry(store.Table())
	id := r.Register(ctx, conn, 16)

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

func TestRegistryClose(t *testing.T) {
	ts := readOnlyServer(t)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	ctx, store := setupTestContext()
	defer store.Close()

	r := NewRegistry(store.Table())
	id := r.Register(ctx, conn, 16)

	err = r.Close(id, 1000, "test close")
	if err != nil {
		t.Errorf("close failed: %v", err)
	}

	_, err = r.Get(id)
	if !errors.Is(err, ErrConnNotFound) {
		t.Errorf("expected ErrConnNotFound after close, got %v", err)
	}
}

func TestRegistryCloseAll(t *testing.T) {
	ts := readOnlyServer(t)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]

	ctx, store := setupTestContext()
	r := NewRegistry(store.Table())

	for i := 0; i < 5; i++ {
		conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
		if err != nil {
			t.Fatalf("dial %d failed: %v", i, err)
		}
		r.Register(ctx, conn, 16)
	}

	store.Close()

	if store.Table().Len() != 0 {
		t.Errorf("expected 0 connections after Close, got %d", store.Table().Len())
	}
}

func TestRegistryDoubleClose(t *testing.T) {
	ts := readOnlyServer(t)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	ctx, store := setupTestContext()
	defer store.Close()

	r := NewRegistry(store.Table())
	id := r.Register(ctx, conn, 16)

	err = r.Close(id, 1000, "first close")
	if err != nil {
		t.Errorf("first close failed: %v", err)
	}

	err = r.Close(id, 1000, "second close")
	if err != ErrConnNotFound {
		t.Errorf("expected ErrConnNotFound on second close, got %v", err)
	}
}

func TestConnectHandler(t *testing.T) {
	ts := readOnlyServer(t)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]

	ctx, store := setupTestContext()
	defer store.Close()

	d := NewDispatcher(4)
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var connID uint64
	done := make(chan struct{})
	err := handlers[wsapi.CmdWsConnect].Handle(ctx, wsapi.WsConnectCmd{URL: wsURL}, 1, &testReceiver{fn: func(_ uint64, data any, _ error) {
		connID = data.(uint64)
		close(done)
	}})

	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for connect")
	}

	if connID == 0 {
		t.Error("expected non-zero connection ID")
	}

	registry := GetRegistry(ctx)
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

func TestConnectHandlerWithHeaders(t *testing.T) {
	var receivedHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(r.Context())
			if err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	d := NewDispatcher(4)
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	done := make(chan struct{})
	err := handlers[wsapi.CmdWsConnect].Handle(ctx, wsapi.WsConnectCmd{
		URL:     wsURL,
		Headers: map[string]string{"X-Custom": "test-value"},
	}, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {
		close(done)
	}})

	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for connect")
	}

	if receivedHeaders.Get("X-Custom") != "test-value" {
		t.Errorf("expected X-Custom header, got %v", receivedHeaders)
	}
}

func TestSendHandler(t *testing.T) {
	var receivedData []byte
	var wg sync.WaitGroup
	wg.Add(1)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		_, data, err := conn.Read(r.Context())
		if err == nil {
			receivedData = data
		}
		wg.Done()
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateRegistry(ctx)
	connID := registry.Register(ctx, conn, 16)

	d := NewDispatcher(4)
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	done := make(chan struct{})
	err = handlers[wsapi.CmdWsSend].Handle(ctx, wsapi.WsSendCmd{
		ConnID:      connID,
		Data:        []byte("hello"),
		MessageType: wsapi.MessageText,
	}, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {
		close(done)
	}})

	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for send")
	}

	wg.Wait()
	if string(receivedData) != "hello" {
		t.Errorf("expected 'hello', got '%s'", receivedData)
	}
}

func TestReceiveHandler(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		_ = conn.Write(r.Context(), websocket.MessageText, []byte("server message"))
		for {
			_, _, err := conn.Read(r.Context())
			if err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateRegistry(ctx)
	connID := registry.Register(ctx, conn, 16)

	// Wait for message to arrive in channel
	time.Sleep(50 * time.Millisecond)

	d := NewDispatcher(4)
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var msg wsapi.WsMessage
	done := make(chan struct{})
	err = handlers[wsapi.CmdWsReceive].Handle(ctx, wsapi.WsReceiveCmd{ConnID: connID}, 1, &testReceiver{fn: func(_ uint64, data any, _ error) {
		msg = data.(wsapi.WsMessage)
		close(done)
	}})

	if err != nil {
		t.Fatalf("receive failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for receive")
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

func TestReceiveHandlerBinary(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		_ = conn.Write(r.Context(), websocket.MessageBinary, []byte{0x00, 0x01, 0x02})
		for {
			_, _, err := conn.Read(r.Context())
			if err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateRegistry(ctx)
	connID := registry.Register(ctx, conn, 16)

	time.Sleep(50 * time.Millisecond)

	d := NewDispatcher(4)
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var msg wsapi.WsMessage
	done := make(chan struct{})
	err = handlers[wsapi.CmdWsReceive].Handle(ctx, wsapi.WsReceiveCmd{ConnID: connID}, 1, &testReceiver{fn: func(_ uint64, data any, _ error) {
		msg = data.(wsapi.WsMessage)
		close(done)
	}})

	if err != nil {
		t.Fatalf("receive failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for receive")
	}

	if msg.MessageType != wsapi.MessageBinary {
		t.Errorf("expected binary message type, got %d", msg.MessageType)
	}
}

func TestReceiveHandlerEOFOnServerClose(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn.Close(websocket.StatusNormalClosure, "closing")
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateRegistry(ctx)
	connID := registry.Register(ctx, conn, 16)

	// Use channel directly to wait for EOF
	msgCh, err := registry.GetMessageChan(connID)
	if err != nil {
		t.Fatalf("get message chan failed: %v", err)
	}

	select {
	case msg, ok := <-msgCh:
		if !ok {
			// Channel closed is also acceptable
			return
		}
		if !msg.EOF {
			t.Error("expected EOF=true on server close")
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for EOF")
	}
}

func TestCloseHandler(t *testing.T) {
	ts := readOnlyServer(t)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateRegistry(ctx)
	connID := registry.Register(ctx, conn, 16)

	d := NewDispatcher(4)
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	done := make(chan struct{})
	err = handlers[wsapi.CmdWsClose].Handle(ctx, wsapi.WsCloseCmd{
		ConnID: connID,
		Code:   1000,
		Reason: "normal close",
	}, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {
		close(done)
	}})

	if err != nil {
		t.Fatalf("close failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for close")
	}

	_, err = registry.Get(connID)
	if err != ErrConnNotFound {
		t.Errorf("expected ErrConnNotFound after close, got %v", err)
	}
}

func TestPingHandler(t *testing.T) {
	ts := readOnlyServer(t)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateRegistry(ctx)
	connID := registry.Register(ctx, conn, 16)

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	d := NewDispatcher(4)
	_ = d.Start(pingCtx)
	defer func() { _ = d.Stop(pingCtx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	done := make(chan struct{})
	err = handlers[wsapi.CmdWsPing].Handle(pingCtx, wsapi.WsPingCmd{ConnID: connID}, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {
		close(done)
	}})

	if err != nil {
		t.Fatalf("ping failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ping")
	}
}

func TestSendHandlerNotFound(t *testing.T) {
	ctx, store := setupTestContext()
	defer store.Close()
	GetOrCreateRegistry(ctx)

	d := NewDispatcher(4)
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var emitted bool
	err := handlers[wsapi.CmdWsSend].Handle(ctx, wsapi.WsSendCmd{
		ConnID: 99999,
		Data:   []byte("test"),
	}, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {
		emitted = true
	}})

	if err != nil {
		t.Errorf("expected no error (silent failure), got %v", err)
	}

	// Give time for async execution
	time.Sleep(50 * time.Millisecond)

	if emitted {
		t.Error("should not emit on not found")
	}
}

func TestReceiveHandlerNotFound(t *testing.T) {
	ctx, store := setupTestContext()
	defer store.Close()
	GetOrCreateRegistry(ctx)

	d := NewDispatcher(4)
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var emitted bool
	err := handlers[wsapi.CmdWsReceive].Handle(ctx, wsapi.WsReceiveCmd{ConnID: 99999}, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {
		emitted = true
	}})

	if err != nil {
		t.Errorf("expected no error (silent failure), got %v", err)
	}

	// Give time for async execution
	time.Sleep(50 * time.Millisecond)

	if emitted {
		t.Error("should not emit on not found")
	}
}

func TestConcurrentSends(t *testing.T) {
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
			_, _, err := conn.Read(r.Context())
			if err != nil {
				break
			}
			if msgCount.Add(1) >= numMessages {
				wg.Done()
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateRegistry(ctx)
	connID := registry.Register(ctx, conn, 16)

	d := NewDispatcher(4)
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var sendWg sync.WaitGroup
	for i := 0; i < numMessages; i++ {
		sendWg.Add(1)
		go func() {
			defer sendWg.Done()
			_ = handlers[wsapi.CmdWsSend].Handle(ctx, wsapi.WsSendCmd{
				ConnID:      connID,
				Data:        []byte("msg"),
				MessageType: wsapi.MessageText,
			}, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {}})
		}()
	}

	sendWg.Wait()
	wg.Wait()

	if msgCount.Load() != numMessages {
		t.Errorf("expected %d messages, got %d", numMessages, msgCount.Load())
	}
}

func TestFullCycle(t *testing.T) {
	ts := echoServer(t)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	d := NewDispatcher(4)
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var connID uint64
	done := make(chan struct{})
	err := handlers[wsapi.CmdWsConnect].Handle(ctx, wsapi.WsConnectCmd{URL: wsURL}, 1, &testReceiver{fn: func(_ uint64, data any, _ error) {
		connID = data.(uint64)
		close(done)
	}})
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	<-done

	done = make(chan struct{})
	err = handlers[wsapi.CmdWsSend].Handle(ctx, wsapi.WsSendCmd{
		ConnID:      connID,
		Data:        []byte("hello"),
		MessageType: wsapi.MessageText,
	}, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {
		close(done)
	}})
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	<-done

	// Wait for echo
	time.Sleep(50 * time.Millisecond)

	var msg wsapi.WsMessage
	done = make(chan struct{})
	err = handlers[wsapi.CmdWsReceive].Handle(ctx, wsapi.WsReceiveCmd{ConnID: connID}, 1, &testReceiver{fn: func(_ uint64, data any, _ error) {
		msg = data.(wsapi.WsMessage)
		close(done)
	}})
	if err != nil {
		t.Fatalf("receive failed: %v", err)
	}
	<-done
	if string(msg.Data) != "hello" {
		t.Errorf("expected 'hello', got '%s'", msg.Data)
	}

	done = make(chan struct{})
	err = handlers[wsapi.CmdWsClose].Handle(ctx, wsapi.WsCloseCmd{
		ConnID: connID,
		Code:   1000,
		Reason: "done",
	}, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {
		close(done)
	}})
	if err != nil {
		t.Fatalf("close failed: %v", err)
	}
	<-done
}

func TestChannelReceiveMultipleMessages(t *testing.T) {
	const numMessages = 5
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		for i := 0; i < numMessages; i++ {
			_ = conn.Write(r.Context(), websocket.MessageText, []byte("msg"))
			time.Sleep(10 * time.Millisecond)
		}
		conn.Close(websocket.StatusNormalClosure, "done")
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateRegistry(ctx)
	connID := registry.Register(ctx, conn, 16)

	// Get message channel directly
	msgCh, err := registry.GetMessageChan(connID)
	if err != nil {
		t.Fatalf("get message chan failed: %v", err)
	}

	var received []wsapi.WsMessage
	timeout := time.After(2 * time.Second)

	for {
		select {
		case msg, ok := <-msgCh:
			if !ok {
				t.Fatal("channel closed unexpectedly")
			}
			received = append(received, msg)
			if msg.EOF {
				goto done
			}
		case <-timeout:
			t.Fatal("timeout waiting for messages")
		}
	}
done:

	// Should have numMessages + 1 EOF
	if len(received) != numMessages+1 {
		t.Errorf("expected %d messages (data + EOF), got %d", numMessages+1, len(received))
	}

	// Last should be EOF
	if !received[len(received)-1].EOF {
		t.Error("last message should be EOF")
	}
}

func TestChannelClosedOnContextCancel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		// Keep connection open
		time.Sleep(10 * time.Second)
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	baseCtx, store := setupTestContext()
	defer store.Close()

	ctx, cancel := context.WithCancel(baseCtx)

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateRegistry(baseCtx)
	connID := registry.Register(ctx, conn, 16)

	msgCh, err := registry.GetMessageChan(connID)
	if err != nil {
		t.Fatalf("get message chan failed: %v", err)
	}

	// Give readLoop time to start and block on Read
	time.Sleep(50 * time.Millisecond)

	// Cancel context - this should interrupt the Read
	cancel()

	// Channel should close or deliver EOF
	select {
	case msg, ok := <-msgCh:
		if ok && !msg.EOF {
			t.Error("expected EOF or closed channel")
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for channel close")
	}
}

func TestCleanupOnStoreClose(t *testing.T) {
	var serverClosed atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(r.Context())
			if err != nil {
				serverClosed.Store(true)
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()

	d := NewDispatcher(4)
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var connID uint64
	done := make(chan struct{})
	err := handlers[wsapi.CmdWsConnect].Handle(ctx, wsapi.WsConnectCmd{URL: wsURL}, 1, &testReceiver{fn: func(_ uint64, data any, _ error) {
		connID = data.(uint64)
		close(done)
	}})
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	<-done

	registry := GetRegistry(ctx)
	if registry == nil {
		t.Fatal("registry should exist")
	}
	_, err = registry.Get(connID)
	if err != nil {
		t.Fatalf("connection should be active: %v", err)
	}

	// Close the store (simulates process end)
	store.Close()

	time.Sleep(100 * time.Millisecond)

	if !serverClosed.Load() {
		t.Error("server should have detected connection close")
	}
}

func TestCleanupMultipleConnections(t *testing.T) {
	var closeCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(r.Context())
			if err != nil {
				closeCount.Add(1)
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()

	d := NewDispatcher(4)
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	for i := 0; i < 5; i++ {
		done := make(chan struct{})
		err := handlers[wsapi.CmdWsConnect].Handle(ctx, wsapi.WsConnectCmd{URL: wsURL}, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {
			close(done)
		}})
		if err != nil {
			t.Fatalf("connect %d failed: %v", i, err)
		}
		<-done
	}

	if store.Table().Len() != 5 {
		t.Errorf("expected 5 connections, got %d", store.Table().Len())
	}

	store.Close()

	time.Sleep(100 * time.Millisecond)

	if closeCount.Load() != 5 {
		t.Errorf("expected 5 connections closed, got %d", closeCount.Load())
	}
}

func TestDispatcher_RegisterAll(t *testing.T) {
	d := NewDispatcher(4)

	handlers := make(map[dispatcher.CommandID]bool)
	d.RegisterAll(func(id dispatcher.CommandID, _ dispatcher.Handler) {
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

func TestDispatcherWithWorkers(t *testing.T) {
	ts := echoServer(t)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	d := NewDispatcher(2)
	err := d.Start(ctx)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var connID uint64
	var wg sync.WaitGroup
	wg.Add(1)
	err = handlers[wsapi.CmdWsConnect].Handle(ctx, wsapi.WsConnectCmd{URL: wsURL}, 1, &testReceiver{fn: func(_ uint64, data any, _ error) {
		connID = data.(uint64)
		wg.Done()
	}})
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	wg.Wait()

	if connID == 0 {
		t.Fatal("expected non-zero connection ID")
	}

	// Send multiple messages concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = handlers[wsapi.CmdWsSend].Handle(ctx, wsapi.WsSendCmd{
				ConnID:      connID,
				Data:        []byte("async"),
				MessageType: wsapi.MessageText,
			}, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {}})
		}()
	}
	wg.Wait()
}

func TestRemoteCloseDeliveryEOF(t *testing.T) {
	// Server sends messages then closes
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		// Send a message first
		_ = conn.Write(r.Context(), websocket.MessageText, []byte("before close"))
		time.Sleep(20 * time.Millisecond)

		// Close connection
		conn.Close(websocket.StatusNormalClosure, "server closing")
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateRegistry(ctx)
	connID := registry.Register(ctx, conn, 16)

	msgCh, err := registry.GetMessageChan(connID)
	if err != nil {
		t.Fatalf("get message chan failed: %v", err)
	}

	var messages []wsapi.WsMessage
	timeout := time.After(2 * time.Second)

loop:
	for {
		select {
		case msg, ok := <-msgCh:
			if !ok {
				break loop
			}
			messages = append(messages, msg)
			if msg.EOF {
				break loop
			}
		case <-timeout:
			t.Fatal("timeout waiting for messages")
		}
	}

	// Should have: 1 data message + 1 EOF
	if len(messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(messages))
	}

	// First should be data
	if len(messages) > 0 && string(messages[0].Data) != "before close" {
		t.Errorf("expected 'before close', got '%s'", messages[0].Data)
	}

	// Second should be EOF
	if len(messages) > 1 && !messages[1].EOF {
		t.Error("expected EOF message")
	}
}

// Benchmarks

func BenchmarkSend(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(r.Context())
			if err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		b.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateRegistry(ctx)
	connID := registry.Register(ctx, conn, 16)

	d := NewDispatcher(4)
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = handlers[wsapi.CmdWsSend].Handle(ctx, wsapi.WsSendCmd{
				ConnID:      connID,
				Data:        []byte("benchmark"),
				MessageType: wsapi.MessageText,
			}, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {}})
		}
	})
}

func BenchmarkReceive(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		// Send messages as fast as possible
		for i := 0; i < b.N*10; i++ {
			if err := conn.Write(r.Context(), websocket.MessageText, []byte("bench")); err != nil {
				return
			}
		}
		// Keep connection open
		time.Sleep(10 * time.Second)
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		b.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateRegistry(ctx)
	connID := registry.Register(ctx, conn, 1024)

	// Wait for buffer to fill
	time.Sleep(100 * time.Millisecond)

	d := NewDispatcher(4)
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handlers[wsapi.CmdWsReceive].Handle(ctx, wsapi.WsReceiveCmd{ConnID: connID}, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {}})
	}
}

func BenchmarkChannelReceive(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		for i := 0; i < b.N*10; i++ {
			if err := conn.Write(r.Context(), websocket.MessageText, []byte("bench")); err != nil {
				return
			}
		}
		time.Sleep(10 * time.Second)
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		b.Fatalf("dial failed: %v", err)
	}

	registry := GetOrCreateRegistry(ctx)
	connID := registry.Register(ctx, conn, 1024)

	msgCh, err := registry.GetMessageChan(connID)
	if err != nil {
		b.Fatalf("get message chan failed: %v", err)
	}

	// Wait for buffer to fill (same as BenchmarkReceive)
	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		<-msgCh
	}
}

// BenchmarkChannelReceiveOnly benchmarks pure channel receive without network
func BenchmarkChannelReceiveOnly(b *testing.B) {
	ch := make(chan wsapi.WsMessage, 1024)

	// Pre-fill channel
	for i := 0; i < 1024; i++ {
		ch <- wsapi.WsMessage{Data: []byte("bench"), MessageType: wsapi.MessageText}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(ch) == 0 {
			// Refill
			for j := 0; j < 1024; j++ {
				ch <- wsapi.WsMessage{Data: []byte("bench"), MessageType: wsapi.MessageText}
			}
		}
		<-ch
	}
}
