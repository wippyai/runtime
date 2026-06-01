// SPDX-License-Identifier: MPL-2.0

package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime/resource"
	wsapi "github.com/wippyai/runtime/api/service/websocket"
	sysrelay "github.com/wippyai/runtime/system/relay"

	coderws "github.com/coder/websocket"
)

// captureHost records relay packages routed to its host so a Subscribe read
// loop's emissions can be inspected.
type captureHost struct {
	mu       sync.Mutex
	terminal int
	messages int
}

func (h *captureHost) Send(pkg *relay.Package) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range pkg.Messages {
		for _, p := range m.Payloads {
			if payload.IsTerminal(p) {
				h.terminal++
			} else {
				h.messages++
			}
		}
	}
	return nil
}

func (h *captureHost) counts() (int, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.messages, h.terminal
}

type subscribeFixture struct {
	ctx      context.Context
	handlers map[dispatcher.CommandID]dispatcher.Handler
	host     *captureHost
	teardown func()
	connID   uint64
}

func newSubscribeFixture(t *testing.T, wsURL string) *subscribeFixture {
	t.Helper()

	const hostID = "ws-sub-test"
	node := sysrelay.NewNode(hostID)
	host := &captureHost{}
	require.NoError(t, node.RegisterHost(hostID, host))

	// Root with an AppContext so relay.WithNode (which stores on the app
	// context) takes effect; the dispatcher read loop resolves the node via
	// relay.GetNode at subscribe time.
	root := ctxapi.NewRootContext()
	root = relay.WithNode(root, node)
	ctx, _ := ctxapi.OpenFrameContext(root)
	store := resource.NewStore()
	require.NoError(t, resource.SetStore(ctx, store))

	d := NewDispatcher()
	require.NoError(t, d.Start(ctx))

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	var connID uint64
	done := make(chan struct{})
	require.NoError(t, handlers[wsapi.Connect].Handle(ctx, wsapi.ConnectCmd{URL: wsURL}, 1, &testReceiver{fn: func(_ uint64, data any, _ error) {
		connID = data.(uint64)
		close(done)
	}}))
	<-done
	require.NotZero(t, connID)

	runPID := pid.PID{Host: hostID, UniqID: "p1"}
	runPID = runPID.Precomputed()

	subDone := make(chan struct{})
	var subErr error
	require.NoError(t, handlers[wsapi.Subscribe].Handle(ctx, wsapi.SubscribeCmd{
		ConnID: connID,
		Topic:  "ws@1",
		PID:    runPID,
	}, 2, &testReceiver{fn: func(_ uint64, _ any, e error) { subErr = e; close(subDone) }}))
	<-subDone
	require.NoError(t, subErr, "subscribe must succeed")

	return &subscribeFixture{
		ctx:      ctx,
		handlers: handlers,
		host:     host,
		connID:   connID,
		teardown: func() {
			_ = d.Stop(ctx)
			_ = store.Close()
		},
	}
}

// Process-initiated close (CloseCmd, the conn:close() path) must NOT relay a
// terminal: the process reclaims the subscription on its step thread, so a
// terminal to the now-unmatched topic would be misrouted (e.g. to the inbox).
func TestSubscribeProcessCloseSuppressesTerminal(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	wsURL := "ws" + srv.URL[4:]

	f := newSubscribeFixture(t, wsURL)
	defer f.teardown()

	// Drive a real echo so a non-terminal message is relayed first.
	sendDone := make(chan struct{})
	require.NoError(t, f.handlers[wsapi.Send].Handle(f.ctx, wsapi.SendCmd{
		ConnID:      f.connID,
		Data:        []byte("hello"),
		MessageType: wsapi.MessageText,
	}, 3, &testReceiver{fn: func(_ uint64, _ any, _ error) { close(sendDone) }}))
	<-sendDone
	time.Sleep(80 * time.Millisecond)

	closeDone := make(chan struct{})
	require.NoError(t, f.handlers[wsapi.Close].Handle(f.ctx, wsapi.CloseCmd{
		ConnID: f.connID,
		Code:   1000,
		Reason: "done",
	}, 4, &testReceiver{fn: func(_ uint64, _ any, _ error) { close(closeDone) }}))
	<-closeDone
	time.Sleep(120 * time.Millisecond)

	messages, terminal := f.host.counts()
	assert.GreaterOrEqual(t, messages, 1, "echo must be relayed (non-vacuous)")
	assert.Zero(t, terminal, "process-initiated close must not relay a terminal")
}

// Remote close (server-initiated) MUST relay a terminal so the process learns
// the stream ended and reclaims the subscription.
func TestSubscribeRemoteCloseRelaysTerminal(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		<-release
		_ = conn.Close(coderws.StatusNormalClosure, "server closing")
	}))
	defer srv.Close()
	wsURL := "ws" + srv.URL[4:]

	f := newSubscribeFixture(t, wsURL)
	defer f.teardown()

	close(release)
	time.Sleep(150 * time.Millisecond)

	_, terminal := f.host.counts()
	assert.GreaterOrEqual(t, terminal, 1, "remote close must relay a terminal to reclaim the subscription")
}
