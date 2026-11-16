// SPDX-License-Identifier: MPL-2.0

package wsrelay

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/relay"
	httpapi "github.com/wippyai/runtime/api/service/http"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type readLimitNode struct {
	messages chan int
	wsRecordingNode
	topics []string
	mu     sync.Mutex
}

func (n *readLimitNode) Send(pkg *relay.Package) error {
	for _, msg := range pkg.Messages {
		n.mu.Lock()
		n.topics = append(n.topics, msg.Topic)
		n.mu.Unlock()
		if msg.Topic != MessageTopic || len(msg.Payloads) == 0 {
			continue
		}
		if text, ok := msg.Payloads[0].Data().(string); ok {
			n.messages <- len(text)
		}
	}
	relay.ReleasePackage(pkg)
	return nil
}

func (n *readLimitNode) topicSnapshot() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.topics...)
}

type closeTrackingListener struct {
	net.Listener
	closed chan struct{}
	once   sync.Once
}

func (l *closeTrackingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &closeTrackingConn{Conn: conn, listener: l}, nil
}

type closeTrackingConn struct {
	net.Conn
	listener *closeTrackingListener
}

func (c *closeTrackingConn) Close() error {
	c.listener.once.Do(func() { close(c.listener.closed) })
	return c.Conn.Close()
}

type readLimitRelay struct {
	node         *readLimitNode
	logs         *observer.ObservedLogs
	served       chan struct{}
	socketClosed chan struct{}
	url          string
}

func startReadLimitRelay(t *testing.T, options map[string]string) *readLimitRelay {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	events := &wsEventLog{}
	host := &wsRecordingHost{events: events}
	node := &readLimitNode{wsRecordingNode: wsRecordingNode{host: host}, messages: make(chan int, 4)}
	manager := &RelayManager{
		appCtx:     context.Background(),
		logger:     zap.New(core),
		pidGen:     &wsPIDGenerator{},
		node:       node,
		topo:       &wsRecordingTopology{events: events},
		transcoder: wsRecordingTranscoder{},
	}
	relayHandler := manager.CreateMiddleware(options)(relayHeaderHandler())
	served := make(chan struct{})
	var once sync.Once
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer once.Do(func() { close(served) })
		ctx, frame := contextapi.OpenFrameContext(req.Context())
		defer contextapi.ReleaseFrameContext(frame)
		if err := frame.Set(httpapi.ServerKey(), host); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := frame.Set(httpapi.ServerIDKey(), "app:test"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		relayHandler.ServeHTTP(w, req.WithContext(ctx))
	}))
	listener := &closeTrackingListener{Listener: server.Listener, closed: make(chan struct{})}
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return &readLimitRelay{node: node, logs: logs, served: served, socketClosed: listener.closed, url: server.URL}
}

func (r *readLimitRelay) send(t *testing.T, size int) *websocket.Conn {
	t.Helper()
	header := http.Header{"Origin": {r.url}}
	conn, _, err := websocket.Dial(t.Context(), websocketURL(r.url), &websocket.DialOptions{HTTPHeader: header}) //nolint:bodyclose // coder/websocket transfers the successful response body to Conn.
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })
	require.NoError(t, conn.Write(t.Context(), websocket.MessageText, []byte(strings.Repeat("x", size))))
	return conn
}

func (r *readLimitRelay) warnings(snippet string) []observer.LoggedEntry {
	return r.logs.FilterLevelExact(zapcore.WarnLevel).FilterMessageSnippet(snippet).All()
}

func TestReadLimit_RelaysMessagesUpToTheLimit(t *testing.T) {
	cases := []struct {
		options map[string]string
		name    string
		size    int
	}{
		{name: "default", size: int(DefaultReadLimit)},
		{name: "configured", options: map[string]string{OptionReadLimit: "65536"}, size: 65536},
		{name: "configured with spaces", options: map[string]string{OptionReadLimit: " 65536 "}, size: 65536},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			relayServer := startReadLimitRelay(t, tc.options)
			relayServer.send(t, tc.size)

			select {
			case size := <-relayServer.node.messages:
				assert.Equal(t, tc.size, size)
			case <-relayServer.served:
				t.Fatal("relay closed the connection instead of relaying the message")
			case <-time.After(5 * time.Second):
				t.Fatal("message was not relayed")
			}
			assert.Empty(t, relayServer.warnings("read limit"))
		})
	}
}

func TestReadLimit_ClosesOversizedMessageWithWarning(t *testing.T) {
	cases := []struct {
		options map[string]string
		name    string
		limit   int64
	}{
		{name: "default", limit: DefaultReadLimit},
		{name: "configured", options: map[string]string{OptionReadLimit: "65536"}, limit: 65536},
		{name: "configured with spaces", options: map[string]string{OptionReadLimit: " 65536 "}, limit: 65536},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			relayServer := startReadLimitRelay(t, tc.options)
			conn := relayServer.send(t, int(tc.limit)+1)

			_, _, err := conn.Read(t.Context())
			assert.Equal(t, websocket.StatusMessageTooBig, websocket.CloseStatus(err))
			select {
			case <-relayServer.served:
			case <-time.After(5 * time.Second):
				t.Fatal("relay handler did not return after an oversized message")
			}
			select {
			case <-relayServer.socketClosed:
			case <-time.After(3 * time.Second):
				t.Fatal("relay kept the server socket open after an oversized message")
			}

			assert.Empty(t, relayServer.node.messages, "an oversized message must not reach the target")
			assert.Contains(t, relayServer.node.topicSnapshot(), LeaveTopic, "the target must learn the client left")
			warnings := relayServer.warnings("exceeds read limit")
			require.Len(t, warnings, 1)
			fields := warnings[0].ContextMap()
			assert.Equal(t, tc.limit, fields["read_limit"])
			assert.Contains(t, fields, "pid")
			assert.Contains(t, fields, "target_pid")
		})
	}
}

func TestReadLimit_InvalidOptionFallsBackToDefault(t *testing.T) {
	invalid := []string{"", "abc", "0", "-1", "1.5", "8MiB", "99999999999999999999", "9223372036854775807"}
	for _, value := range invalid {
		t.Run(value, func(t *testing.T) {
			relayServer := startReadLimitRelay(t, map[string]string{OptionReadLimit: value})

			warnings := relayServer.warnings("invalid read limit")
			require.Len(t, warnings, 1)
			fields := warnings[0].ContextMap()
			assert.Equal(t, OptionReadLimit, fields["option"])
			assert.Equal(t, value, fields["value"])
			assert.Equal(t, DefaultReadLimit, fields["read_limit"])

			conn := relayServer.send(t, int(DefaultReadLimit)+1)
			_, _, err := conn.Read(t.Context())
			assert.Equal(t, websocket.StatusMessageTooBig, websocket.CloseStatus(err))
		})
	}
}
