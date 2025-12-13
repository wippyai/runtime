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
	wssvc "github.com/wippyai/runtime/api/service/websocket"
	wsapi "github.com/wippyai/runtime/api/websocket"

	"github.com/coder/websocket"
	"go.uber.org/zap"
)

// testReceiver implements dispatcher.ResultReceiver for tests.
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

func TestConnectHandler(t *testing.T) {
	ts := readOnlyServer(t)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]

	ctx, store := setupTestContext()
	defer store.Close()

	d := NewDispatcher()
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

	msgCh, err := registry.GetMessageChan(connID)
	if err != nil {
		t.Fatalf("get connection failed: %v", err)
	}
	if msgCh == nil {
		t.Error("expected non-nil message channel")
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

	d := NewDispatcher()
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

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetRegistry(ctx)
	connID := registry.Register(ctx, conn, 16, 0)

	d := NewDispatcher()
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

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetRegistry(ctx)
	connID := registry.Register(ctx, conn, 16, 0)

	// Wait for message to arrive in channel
	time.Sleep(50 * time.Millisecond)

	d := NewDispatcher()
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

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetRegistry(ctx)
	connID := registry.Register(ctx, conn, 16, 0)

	time.Sleep(50 * time.Millisecond)

	d := NewDispatcher()
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

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetRegistry(ctx)
	connID := registry.Register(ctx, conn, 16, 0)

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

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetRegistry(ctx)
	connID := registry.Register(ctx, conn, 16, 0)

	d := NewDispatcher()
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

	_, err = registry.GetMessageChan(connID)
	if !errors.Is(err, wssvc.ErrConnNotFound) {
		t.Errorf("expected ErrConnNotFound after close, got %v", err)
	}
}

func TestPingHandler(t *testing.T) {
	ts := readOnlyServer(t)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetRegistry(ctx)
	connID := registry.Register(ctx, conn, 16, 0)

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	d := NewDispatcher()
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
	GetRegistry(ctx)

	d := NewDispatcher()
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var gotErr atomic.Value
	err := handlers[wsapi.CmdWsSend].Handle(ctx, wsapi.WsSendCmd{
		ConnID: 99999,
		Data:   []byte("test"),
	}, 1, &testReceiver{fn: func(_ uint64, _ any, e error) {
		gotErr.Store(e)
	}})

	if err != nil {
		t.Errorf("Handle should return nil, got %v", err)
	}

	// Give time for async execution
	time.Sleep(50 * time.Millisecond)

	if gotErr.Load() == nil {
		t.Error("should complete with error on not found")
	}
}

func TestReceiveHandlerNotFound(t *testing.T) {
	ctx, store := setupTestContext()
	defer store.Close()
	GetRegistry(ctx)

	d := NewDispatcher()
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var gotErr atomic.Value
	err := handlers[wsapi.CmdWsReceive].Handle(ctx, wsapi.WsReceiveCmd{ConnID: 99999}, 1, &testReceiver{fn: func(_ uint64, _ any, e error) {
		gotErr.Store(e)
	}})

	if err != nil {
		t.Errorf("Handle should return nil, got %v", err)
	}

	// Give time for async execution
	time.Sleep(50 * time.Millisecond)

	if gotErr.Load() == nil {
		t.Error("should complete with error on not found")
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

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetRegistry(ctx)
	connID := registry.Register(ctx, conn, 16, 0)

	d := NewDispatcher()
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

	d := NewDispatcher()
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

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetRegistry(ctx)
	connID := registry.Register(ctx, conn, 16, 0)

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

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetRegistry(baseCtx)
	connID := registry.Register(ctx, conn, 16, 0)

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

	d := NewDispatcher()
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
	_, err = registry.GetMessageChan(connID)
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

	d := NewDispatcher()
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
	d := NewDispatcher()

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

	d := NewDispatcher(WithWorkers(2))
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

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetRegistry(ctx)
	connID := registry.Register(ctx, conn, 16, 0)

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

func TestDispatcherWithLogger(t *testing.T) {
	d := NewDispatcher(WithLogger(zap.NewNop()))
	if d.log == nil {
		t.Error("expected logger to be set")
	}
}

func TestNoRegistryErrors(t *testing.T) {
	// Context without resource.Store
	ctx := context.Background()

	d := NewDispatcher()
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	testCases := []struct {
		name string
		cmd  dispatcher.Command
		id   dispatcher.CommandID
	}{
		{"send", wsapi.WsSendCmd{ConnID: 1, Data: []byte("x")}, wsapi.CmdWsSend},
		{"receive", wsapi.WsReceiveCmd{ConnID: 1}, wsapi.CmdWsReceive},
		{"close", wsapi.WsCloseCmd{ConnID: 1}, wsapi.CmdWsClose},
		{"ping", wsapi.WsPingCmd{ConnID: 1}, wsapi.CmdWsPing},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var gotErr error
			done := make(chan struct{})
			_ = handlers[tc.id].Handle(ctx, tc.cmd, 1, &testReceiver{fn: func(_ uint64, _ any, err error) {
				gotErr = err
				close(done)
			}})

			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("timeout")
			}

			if gotErr == nil {
				t.Errorf("%s: expected error for no registry", tc.name)
			}
		})
	}
}

func TestConnectWithProtocols(t *testing.T) {
	var receivedProtocol string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedProtocol = r.Header.Get("Sec-Websocket-Protocol")
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols: []string{"proto1"},
		})
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

	d := NewDispatcher()
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	done := make(chan struct{})
	_ = handlers[wsapi.CmdWsConnect].Handle(ctx, wsapi.WsConnectCmd{
		URL:       wsURL,
		Protocols: []string{"proto1", "proto2"},
	}, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {
		close(done)
	}})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	if receivedProtocol == "" {
		t.Error("expected Sec-Websocket-Protocol header")
	}
}

func TestConnectWithCompression(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			CompressionMode: websocket.CompressionContextTakeover,
		})
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

	d := NewDispatcher()
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	compressionModes := []int{
		wsapi.CompressionContextTakeover,
		wsapi.CompressionNoContext,
		99, // Unknown mode - should fall back to disabled
	}

	for _, mode := range compressionModes {
		done := make(chan struct{})
		var connID uint64
		var gotErr error
		_ = handlers[wsapi.CmdWsConnect].Handle(ctx, wsapi.WsConnectCmd{
			URL:             wsURL,
			CompressionMode: mode,
		}, 1, &testReceiver{fn: func(_ uint64, data any, err error) {
			if data != nil {
				connID = data.(uint64)
			}
			gotErr = err
			close(done)
		}})

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout for compression mode %d", mode)
		}

		if gotErr != nil {
			t.Errorf("connect with compression mode %d failed: %v", mode, gotErr)
		}
		if connID == 0 {
			t.Errorf("expected connection ID for compression mode %d", mode)
		}
	}
}

func TestConnectWithDialTimeout(t *testing.T) {
	ctx, store := setupTestContext()
	defer store.Close()

	d := NewDispatcher()
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	done := make(chan struct{})
	var gotErr error
	_ = handlers[wsapi.CmdWsConnect].Handle(ctx, wsapi.WsConnectCmd{
		URL:         "ws://192.0.2.1:12345", // Non-routable IP
		DialTimeout: 50 * time.Millisecond,
	}, 1, &testReceiver{fn: func(_ uint64, _ any, err error) {
		gotErr = err
		close(done)
	}})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	if gotErr == nil {
		t.Error("expected dial timeout error")
	}
}

func TestConnectWithReadLimit(t *testing.T) {
	ts := readOnlyServer(t)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	d := NewDispatcher()
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	done := make(chan struct{})
	var connID uint64
	_ = handlers[wsapi.CmdWsConnect].Handle(ctx, wsapi.WsConnectCmd{
		URL:       wsURL,
		ReadLimit: 1024,
	}, 1, &testReceiver{fn: func(_ uint64, data any, _ error) {
		connID = data.(uint64)
		close(done)
	}})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	if connID == 0 {
		t.Error("expected non-zero connection ID")
	}
}

func TestSendBinaryMessage(t *testing.T) {
	var receivedType websocket.MessageType
	var wg sync.WaitGroup
	wg.Add(1)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		msgType, _, err := conn.Read(r.Context())
		if err == nil {
			receivedType = msgType
		}
		wg.Done()
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	ctx, store := setupTestContext()
	defer store.Close()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	registry := GetRegistry(ctx)
	connID := registry.Register(ctx, conn, 16, 0)

	d := NewDispatcher()
	_ = d.Start(ctx)
	defer func() { _ = d.Stop(ctx) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	done := make(chan struct{})
	_ = handlers[wsapi.CmdWsSend].Handle(ctx, wsapi.WsSendCmd{
		ConnID:      connID,
		Data:        []byte{0x00, 0x01, 0x02},
		MessageType: wsapi.MessageBinary,
	}, 1, &testReceiver{fn: func(_ uint64, _ any, _ error) {
		close(done)
	}})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	wg.Wait()
	if receivedType != websocket.MessageBinary {
		t.Errorf("expected binary message, got %v", receivedType)
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

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		b.Fatalf("dial failed: %v", err)
	}

	registry := GetRegistry(ctx)
	connID := registry.Register(ctx, conn, 16, 0)

	d := NewDispatcher()
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

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		b.Fatalf("dial failed: %v", err)
	}

	registry := GetRegistry(ctx)
	connID := registry.Register(ctx, conn, 1024, 0)

	// Wait for buffer to fill
	time.Sleep(100 * time.Millisecond)

	d := NewDispatcher()
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

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		b.Fatalf("dial failed: %v", err)
	}

	registry := GetRegistry(ctx)
	connID := registry.Register(ctx, conn, 1024, 0)

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
