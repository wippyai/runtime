// SPDX-License-Identifier: MPL-2.0

package wsrelay

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	httpapi "github.com/wippyai/runtime/api/service/http"
	"go.uber.org/zap"
)

const validRelayConfig = `{"target_pid":"{target|1}"}`

func TestW01SameOriginDefaultEnforcement(t *testing.T) {
	manager := NewWebSocketRelay(context.Background(), zap.NewNop(), nil)
	server := newOriginTestServer(t, manager.CreateMiddleware(nil))
	defer server.Close()

	matchingHeader := http.Header{"Origin": {server.URL}}
	matchingConn, matchingResponse, err := websocket.Dial(t.Context(), websocketURL(server.URL), &websocket.DialOptions{HTTPHeader: matchingHeader}) //nolint:bodyclose // coder/websocket transfers the successful response body to Conn.
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, matchingResponse.StatusCode)
	matchingConn.CloseNow()

	foreignHeader := http.Header{"Origin": {"https://foreign.example"}}
	foreignConn, foreignResponse, err := websocket.Dial(t.Context(), websocketURL(server.URL), &websocket.DialOptions{HTTPHeader: foreignHeader})
	require.Error(t, err)
	assert.Nil(t, foreignConn)
	require.NotNil(t, foreignResponse)
	defer foreignResponse.Body.Close()
	assert.Equal(t, http.StatusForbidden, foreignResponse.StatusCode)
}

func TestW02ExplicitOriginMiddlewareEnforcement(t *testing.T) {
	manager := NewWebSocketRelay(context.Background(), zap.NewNop(), nil)
	middleware := manager.CreateMiddleware(map[string]string{
		OptionAllowedOrigins: "  https://listed.example  ,  https://second.example  ",
	})
	server := newOriginTestServer(t, middleware)
	defer server.Close()

	listedHeader := http.Header{"Origin": {"https://listed.example"}}
	listedConn, listedResponse, err := websocket.Dial(t.Context(), websocketURL(server.URL), &websocket.DialOptions{HTTPHeader: listedHeader}) //nolint:bodyclose // coder/websocket transfers the successful response body to Conn.
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, listedResponse.StatusCode)
	listedConn.CloseNow()

	unlistedHeader := http.Header{"Origin": {"https://unlisted.example"}}
	unlistedConn, unlistedResponse, err := websocket.Dial(t.Context(), websocketURL(server.URL), &websocket.DialOptions{HTTPHeader: unlistedHeader})
	require.Error(t, err)
	assert.Nil(t, unlistedConn)
	require.NotNil(t, unlistedResponse)
	defer unlistedResponse.Body.Close()
	assert.Equal(t, http.StatusForbidden, unlistedResponse.StatusCode)
}

func TestW04MalformedRelayHeaderReturnsBadRequest(t *testing.T) {
	events := &wsEventLog{}
	manager := newRecordingRelayManager(context.Background(), events)
	handler := manager.middlewareHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(RelayHeader, "{malformed")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("downstream response must not commit"))
	}), nil)
	writer := newUpgradeRecordingWriter()

	handler.ServeHTTP(writer, websocketRequest("http://example.com/relay"))

	assert.Equal(t, http.StatusBadRequest, writer.Code)
	assert.Contains(t, writer.Body.String(), "Invalid relay configuration")
	assert.NotContains(t, writer.Body.String(), "downstream response")
	assert.Zero(t, writer.hijacks.Load(), "malformed configuration must fail before websocket upgrade")
	assert.Empty(t, events.snapshot(), "malformed configuration must not attach")
	assert.Zero(t, manager.topo.(*wsRecordingTopology).operationCount(), "malformed configuration must not mutate topology")
}

func TestW05RelayPrerequisitesFailBeforeUpgrade(t *testing.T) {
	type prerequisiteCase struct {
		prepare  func(*testing.T, *http.Request) (*http.Request, contextapi.FrameContext)
		wantBody string
	}
	cases := []prerequisiteCase{
		{
			prepare: func(_ *testing.T, req *http.Request) (*http.Request, contextapi.FrameContext) {
				return req, nil
			},
			wantBody: ErrFrameContextNotFound.Error(),
		},
		{
			prepare: func(t *testing.T, req *http.Request) (*http.Request, contextapi.FrameContext) {
				ctx, frame := contextapi.OpenFrameContext(req.Context())
				require.NoError(t, frame.Set(httpapi.ServerIDKey(), "app:test"))
				return req.WithContext(ctx), frame
			},
			wantBody: ErrServerHostNotFound.Error(),
		},
		{
			prepare: func(t *testing.T, req *http.Request) (*http.Request, contextapi.FrameContext) {
				ctx, frame := contextapi.OpenFrameContext(req.Context())
				require.NoError(t, frame.Set(httpapi.ServerKey(), struct{}{}))
				require.NoError(t, frame.Set(httpapi.ServerIDKey(), "app:test"))
				return req.WithContext(ctx), frame
			},
			wantBody: ErrHostNotAttachable.Error(),
		},
	}

	for _, tc := range cases {
		events := &wsEventLog{}
		manager := newRecordingRelayManager(context.Background(), events)
		handler := manager.middlewareHandler(relayHeaderHandler(), nil)
		req, frame := tc.prepare(t, websocketRequest("http://example.com/relay"))
		writer := newUpgradeRecordingWriter()

		handler.ServeHTTP(writer, req)
		contextapi.ReleaseFrameContext(frame)

		assert.Equal(t, http.StatusInternalServerError, writer.Code)
		assert.Contains(t, writer.Body.String(), tc.wantBody)
		assert.Zero(t, writer.hijacks.Load(), "prerequisite failure must occur before websocket upgrade")
		assert.Empty(t, events.snapshot(), "prerequisite failure must not attach")
		assert.Zero(t, manager.topo.(*wsRecordingTopology).operationCount(), "prerequisite failure must not mutate topology")
	}
}

func TestW06RelayUpgradeCleanupOwnership(t *testing.T) {
	events := &wsEventLog{}
	appCtx, appFrame := contextapi.OpenFrameContext(context.Background())
	frameProbeKey := &contextapi.Key{Name: "wsrelay.frame.release.probe", Inherit: true}
	require.NoError(t, appFrame.Set(frameProbeKey, wsFrameReleaseProbe{events: events}))
	appFrame.Seal()
	defer contextapi.ReleaseFrameContext(appFrame)

	manager := newRecordingRelayManager(appCtx, events)
	done := make(chan struct{})
	relayHandler := manager.CreateMiddleware(nil)(relayHeaderHandler())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer close(done)
		ctx, frame := contextapi.OpenFrameContext(req.Context())
		defer contextapi.ReleaseFrameContext(frame)
		if err := frame.Set(httpapi.ServerKey(), manager.node.(*wsRecordingNode).host); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := frame.Set(httpapi.ServerIDKey(), "app:test"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		relayHandler.ServeHTTP(w, req.WithContext(ctx))
		events.add("handler-return")
	}))
	defer server.Close()

	header := http.Header{"Origin": {server.URL}}
	conn, response, err := websocket.Dial(t.Context(), websocketURL(server.URL), &websocket.DialOptions{HTTPHeader: header}) //nolint:bodyclose // coder/websocket transfers the successful response body to Conn.
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, response.StatusCode)
	require.NoError(t, conn.Close(websocket.StatusNormalClosure, "peer closed"))

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		t.Fatal("relay handler did not return after peer close")
	}

	assert.Equal(t, []string{"attach", "register", "complete", "detach", "frame-close", "handler-return"}, events.snapshot())
	host := manager.node.(*wsRecordingNode).host
	assert.Equal(t, 1, host.attachCount())
	assert.Equal(t, 1, host.detachCount())
	topo := manager.topo.(*wsRecordingTopology)
	assert.Equal(t, 1, topo.registerCount())
	assert.Equal(t, 1, topo.completeCount())
	assert.Equal(t, 1, events.count("frame-close"))
	assert.Equal(t, 1, events.count("handler-return"), "the upgraded socket must have one serving owner")
}

func newOriginTestServer(t *testing.T, middleware func(http.Handler) http.Handler) *httptest.Server {
	t.Helper()
	host := &wsRecordingHost{}
	relayHandler := middleware(relayHeaderHandler())
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
}

func relayHeaderHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(RelayHeader, validRelayConfig)
	})
}

func websocketURL(serverURL string) string {
	return "ws" + strings.TrimPrefix(serverURL, "http")
}

func websocketRequest(rawURL string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, rawURL, nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Origin", "http://example.com")
	return req
}

type upgradeRecordingWriter struct {
	*httptest.ResponseRecorder
	hijacks atomic.Int32
}

func newUpgradeRecordingWriter() *upgradeRecordingWriter {
	return &upgradeRecordingWriter{ResponseRecorder: httptest.NewRecorder()}
}

func (w *upgradeRecordingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacks.Add(1)
	return nil, nil, errors.New("unexpected websocket hijack")
}

type wsEventLog struct {
	events []string
	mu     sync.Mutex
}

func (l *wsEventLog) add(event string) {
	l.mu.Lock()
	l.events = append(l.events, event)
	l.mu.Unlock()
}

func (l *wsEventLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

func (l *wsEventLog) count(want string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	count := 0
	for _, event := range l.events {
		if event == want {
			count++
		}
	}
	return count
}

type wsFrameReleaseProbe struct {
	events *wsEventLog
}

func (p wsFrameReleaseProbe) Clone() any {
	return contextapi.CloserFunc(func() error {
		p.events.add("frame-close")
		return nil
	})
}

type wsRecordingHost struct {
	events   *wsEventLog
	mu       sync.Mutex
	attaches int
	detaches int
}

func (h *wsRecordingHost) Send(_ *relay.Package) error {
	return nil
}

func (h *wsRecordingHost) Attach(_ pid.PID, _ chan *relay.Package) (context.CancelFunc, error) {
	h.mu.Lock()
	h.attaches++
	h.mu.Unlock()
	if h.events != nil {
		h.events.add("attach")
	}
	return func() {}, nil
}

func (h *wsRecordingHost) Detach(_ pid.PID) {
	h.mu.Lock()
	h.detaches++
	h.mu.Unlock()
	if h.events != nil {
		h.events.add("detach")
	}
}

func (h *wsRecordingHost) attachCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attaches
}

func (h *wsRecordingHost) detachCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.detaches
}

type wsRecordingNode struct {
	host *wsRecordingHost
}

func (n *wsRecordingNode) ID() pid.NodeID                                    { return "node" }
func (n *wsRecordingNode) RegisterHost(_ pid.HostID, _ relay.Receiver) error { return nil }
func (n *wsRecordingNode) UnregisterHost(_ pid.HostID)                       {}
func (n *wsRecordingNode) GetHost(_ pid.HostID) (relay.Receiver, bool)       { return nil, false }
func (n *wsRecordingNode) Attach(_ pid.PID, _ chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (n *wsRecordingNode) Detach(_ pid.PID) {}
func (n *wsRecordingNode) Send(pkg *relay.Package) error {
	relay.ReleasePackage(pkg)
	return nil
}

type wsRecordingTopology struct {
	events     *wsEventLog
	mu         sync.Mutex
	registers  int
	completes  int
	operations int
}

func (t *wsRecordingTopology) Register(_ pid.PID) error {
	t.mu.Lock()
	t.registers++
	t.operations++
	t.mu.Unlock()
	t.events.add("register")
	return nil
}

func (t *wsRecordingTopology) Complete(_ pid.PID, _ *runtimeapi.Result) {
	t.mu.Lock()
	t.completes++
	t.operations++
	t.mu.Unlock()
	t.events.add("complete")
}

func (t *wsRecordingTopology) Remove(_ pid.PID) {
	t.mu.Lock()
	t.operations++
	t.mu.Unlock()
}

func (t *wsRecordingTopology) Monitor(_, _ pid.PID) error {
	t.mu.Lock()
	t.operations++
	t.mu.Unlock()
	return nil
}

func (t *wsRecordingTopology) Demonitor(_, _ pid.PID) error {
	t.mu.Lock()
	t.operations++
	t.mu.Unlock()
	return nil
}

func (t *wsRecordingTopology) Link(_, _ pid.PID) error {
	t.mu.Lock()
	t.operations++
	t.mu.Unlock()
	return nil
}

func (t *wsRecordingTopology) Unlink(_, _ pid.PID) error {
	t.mu.Lock()
	t.operations++
	t.mu.Unlock()
	return nil
}

func (t *wsRecordingTopology) GetLinks(_ pid.PID) []pid.PID {
	t.mu.Lock()
	t.operations++
	t.mu.Unlock()
	return nil
}

func (t *wsRecordingTopology) registerCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.registers
}

func (t *wsRecordingTopology) completeCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.completes
}

func (t *wsRecordingTopology) operationCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.operations
}

type wsRecordingTranscoder struct{}

func (wsRecordingTranscoder) Unmarshal(_ payload.Payload, _ interface{}) error {
	return errors.New("unexpected unmarshal")
}

func (wsRecordingTranscoder) Transcode(_ payload.Payload, _ payload.Format) (payload.Payload, error) {
	return nil, errors.New("unexpected transcode")
}

type wsPIDGenerator struct {
	n atomic.Uint64
}

func (g *wsPIDGenerator) Generate(host pid.HostID) pid.PID {
	generated := pid.PID{Node: "node", Host: host, UniqID: fmt.Sprintf("ws-%d", g.n.Add(1))}
	return generated.Precomputed()
}

func newRecordingRelayManager(appCtx context.Context, events *wsEventLog) *RelayManager {
	host := &wsRecordingHost{events: events}
	return &RelayManager{
		appCtx:     appCtx,
		logger:     zap.NewNop(),
		pidGen:     &wsPIDGenerator{},
		node:       &wsRecordingNode{host: host},
		topo:       &wsRecordingTopology{events: events},
		transcoder: wsRecordingTranscoder{},
	}
}
