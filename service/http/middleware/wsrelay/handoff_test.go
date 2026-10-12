// SPDX-License-Identifier: MPL-2.0

package wsrelay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

type handoffDelivery struct {
	source pid.PID
	target pid.PID
	topic  string
	join   JoinInfo
}

type handoffNode struct {
	beforeSend func(*relay.Package) error
	wsRecordingNode
	deliveries []handoffDelivery
	mu         sync.Mutex
}

func (n *handoffNode) Send(pkg *relay.Package) error {
	if n.beforeSend != nil {
		if err := n.beforeSend(pkg); err != nil {
			return err
		}
	}
	n.mu.Lock()
	for _, msg := range pkg.Messages {
		delivery := handoffDelivery{source: pkg.Source, target: pkg.Target, topic: msg.Topic}
		if msg.Topic == JoinTopic || msg.Topic == LeaveTopic {
			if err := json.Unmarshal(msg.Payloads[0].Data().([]byte), &delivery.join); err != nil {
				n.mu.Unlock()
				return err
			}
		}
		n.deliveries = append(n.deliveries, delivery)
	}
	n.mu.Unlock()
	relay.ReleasePackage(pkg)
	return nil
}

func (n *handoffNode) snapshot() []handoffDelivery {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]handoffDelivery(nil), n.deliveries...)
}

type handoffTopology struct {
	monitor   func() error
	demonitor func(pid.PID) error
	wsRecordingTopology
}

type handoffTranscoder struct{ wsRecordingTranscoder }

func (handoffTranscoder) Unmarshal(p payload.Payload, target interface{}) error {
	return json.Unmarshal(p.Data().([]byte), target)
}

func (t *handoffTopology) Monitor(_, _ pid.PID) error {
	if t.monitor != nil {
		return t.monitor()
	}
	return nil
}

func (t *handoffTopology) Demonitor(_, target pid.PID) error {
	if t.demonitor != nil {
		return t.demonitor(target)
	}
	return nil
}

func handoffPID(id string) pid.PID {
	p := pid.PID{Node: "node", Host: "app:host", UniqID: id}
	return p.Precomputed()
}

func handoffAddress(id string) string {
	p := handoffPID(id)
	return p.String()
}

func newHandoffConnection(t *testing.T) (*Connection, *handoffNode, *handoffTopology) {
	t.Helper()
	node := &handoffNode{}
	topo := &handoffTopology{wsRecordingTopology: wsRecordingTopology{events: &wsEventLog{}}}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	conn := &Connection{
		ctx: ctx, cancelCtx: cancel, node: node, topo: topo, logger: zap.NewNop(),
		transcoder:       handoffTranscoder{},
		currentTargetPID: handoffPID("old"), wsPID: handoffPID("client"),
		currentMessageTopic: MessageTopic,
	}
	return conn, node, topo
}

func waitHandoff(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handoff deadline")
	}
}

func newHandoffRelease(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	signal := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(signal) }) }
	t.Cleanup(release)
	return signal, release
}

func attachHandoffSocket(t *testing.T, conn *Connection) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err == nil {
			accepted <- ws
		}
	}))
	t.Cleanup(server.Close)
	peer, response, err := websocket.Dial(t.Context(), websocketURL(server.URL), nil)
	if err != nil && response != nil && response.Body != nil {
		require.NoError(t, response.Body.Close())
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.CloseNow() })
	conn.conn = <-accepted
	t.Cleanup(func() { _ = conn.conn.CloseNow() })
	go func() {
		_, _, _ = peer.Read(t.Context())
	}()
}

func TestHandoff_JoinsBeforePublishingNewTarget(t *testing.T) {
	conn, node, topo := newHandoffConnection(t)
	entered, done := make(chan struct{}), make(chan struct{})
	releaseSignal, release := newHandoffRelease(t)
	topo.monitor = func() error { close(entered); <-releaseSignal; return nil }
	go func() {
		defer close(done)
		conn.handleTargetPIDChange(RelayCommand{TargetPID: handoffAddress("new")})
	}()
	waitHandoff(t, entered)
	err := conn.forwardMessageToRelay(websocket.MessageText, []byte("during"))
	release()
	waitHandoff(t, done)
	require.NoError(t, err)
	require.NoError(t, conn.forwardMessageToRelay(websocket.MessageText, []byte("after")))
	delivered := node.snapshot()
	require.Len(t, delivered, 4)
	require.Equal(t, handoffPID("old"), delivered[0].target, "commands stay with the joined target during topology preparation")
	require.Equal(t, MessageTopic, delivered[0].topic)
	require.Equal(t, LeaveTopic, delivered[1].topic)
	require.Equal(t, JoinTopic, delivered[2].topic)
	require.Equal(t, handoffPID("new"), delivered[2].target)
	require.Equal(t, handoffPID("new"), delivered[3].target)
	require.Equal(t, MessageTopic, delivered[3].topic)
	for _, delivery := range delivered {
		require.Equal(t, conn.wsPID, delivery.source)
	}
}

func TestHandoff_WaitsForInFlightOldTargetSend(t *testing.T) {
	conn, node, topo := newHandoffConnection(t)
	entered, oldDone, prepared, switched := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	releaseSignal, release := newHandoffRelease(t)
	node.beforeSend = func(pkg *relay.Package) error {
		if pkg.Target == handoffPID("old") && pkg.Messages[0].Topic == MessageTopic {
			close(entered)
			<-releaseSignal
		}
		return nil
	}
	topo.monitor = func() error { close(prepared); return nil }
	var sendErr error
	go func() {
		defer close(oldDone)
		sendErr = conn.forwardMessageToRelay(websocket.MessageText, []byte("in-flight"))
	}()
	waitHandoff(t, entered)
	go func() {
		defer close(switched)
		conn.handleTargetPIDChange(RelayCommand{TargetPID: handoffAddress("new")})
	}()
	waitHandoff(t, prepared)
	// A blocked enqueue must finish before LEAVE. Give the handoff an opportunity
	// to finish incorrectly, then release the enqueue; the final FIFO is decisive.
	select {
	case <-switched:
	case <-time.After(20 * time.Millisecond):
	}
	release()
	waitHandoff(t, oldDone)
	waitHandoff(t, switched)
	require.NoError(t, sendErr)
	delivered := node.snapshot()
	require.Len(t, delivered, 3)
	require.Equal(t, MessageTopic, delivered[0].topic)
	require.Equal(t, LeaveTopic, delivered[1].topic)
	require.Equal(t, JoinTopic, delivered[2].topic)
}

func TestHandoff_PublishesTopicAndMetadataWithTarget(t *testing.T) {
	conn, node, _ := newHandoffConnection(t)
	conn.handleTargetPIDChange(RelayCommand{
		TargetPID: handoffAddress("new"), MessageTopic: "custom.message",
		Metadata: map[string]any{"user_id": "alice"},
	})
	require.NoError(t, conn.forwardMessageToRelay(websocket.MessageText, []byte("after")))
	delivered := node.snapshot()
	require.Len(t, delivered, 3)
	require.Equal(t, "alice", delivered[1].join.Metadata["user_id"])
	require.Equal(t, conn.wsPID.String(), delivered[1].join.ClientPID)
	require.Equal(t, "custom.message", delivered[2].topic)
	require.Equal(t, handoffPID("new"), delivered[2].target)
}

func TestHandoff_UnchangedTargetDoesNotLeaveOrRejoin(t *testing.T) {
	conn, node, _ := newHandoffConnection(t)
	conn.handleTargetPIDChange(RelayCommand{TargetPID: conn.currentTargetPID.String(), Metadata: map[string]any{"user_id": "alice"}})
	require.Empty(t, node.snapshot())
	require.Equal(t, "alice", conn.config.Metadata["user_id"])
}

func TestHandoff_InvalidTargetPreservesRouting(t *testing.T) {
	conn, node, _ := newHandoffConnection(t)
	conn.handleTargetPIDChange(RelayCommand{TargetPID: "invalid"})
	require.Equal(t, handoffPID("old"), conn.currentTargetPID)
	require.Empty(t, node.snapshot())
}

func TestHandoff_FailedMonitorDoesNotPublishNewTarget(t *testing.T) {
	conn, node, topo := newHandoffConnection(t)
	attachHandoffSocket(t, conn)
	topo.monitor = func() error { return errors.New("monitor failed") }
	conn.handleTargetPIDChange(RelayCommand{TargetPID: handoffAddress("new")})
	require.ErrorIs(t, conn.ctx.Err(), context.Canceled)
	require.Equal(t, handoffPID("old"), conn.currentTargetPID)
	require.Empty(t, node.snapshot())
}

func TestHandoff_CanceledPreparationDoesNotPublishOrRetainNewTarget(t *testing.T) {
	conn, node, topo := newHandoffConnection(t)
	var removed []pid.PID
	topo.monitor = func() error { conn.cancelCtx(); return nil }
	topo.demonitor = func(target pid.PID) error { removed = append(removed, target); return nil }
	conn.handleTargetPIDChange(RelayCommand{TargetPID: handoffAddress("new")})
	require.Equal(t, handoffPID("old"), conn.currentTargetPID)
	require.Empty(t, node.snapshot())
	require.Equal(t, []pid.PID{handoffPID("old"), handoffPID("new")}, removed)
}

func TestHandoff_FailedJoinClosesAndReleasesNewMonitor(t *testing.T) {
	conn, node, topo := newHandoffConnection(t)
	attachHandoffSocket(t, conn)
	var removed []pid.PID
	topo.demonitor = func(target pid.PID) error { removed = append(removed, target); return nil }
	node.beforeSend = func(pkg *relay.Package) error {
		if pkg.Messages[0].Topic == JoinTopic {
			return errors.New("join failed")
		}
		return nil
	}
	conn.handleTargetPIDChange(RelayCommand{TargetPID: handoffAddress("new")})
	require.ErrorIs(t, conn.ctx.Err(), context.Canceled)
	require.Equal(t, handoffPID("old"), conn.currentTargetPID)
	require.Equal(t, []pid.PID{handoffPID("old"), handoffPID("new")}, removed)
	require.ErrorIs(t, conn.forwardMessageToRelay(websocket.MessageText, []byte("late")), context.Canceled)
	require.Len(t, node.snapshot(), 1)
}

func TestHandoff_FailedLeaveStillJoinsReachableNewTarget(t *testing.T) {
	conn, node, _ := newHandoffConnection(t)
	node.beforeSend = func(pkg *relay.Package) error {
		if pkg.Messages[0].Topic == LeaveTopic {
			return errors.New("old target gone")
		}
		return nil
	}
	conn.handleTargetPIDChange(RelayCommand{TargetPID: handoffAddress("new")})
	require.NoError(t, conn.forwardMessageToRelay(websocket.MessageText, []byte("after")))
	delivered := node.snapshot()
	require.Len(t, delivered, 2)
	require.Equal(t, JoinTopic, delivered[0].topic)
	require.Equal(t, handoffPID("new"), delivered[1].target)
}

func TestHandoff_MetadataOnlyControlDoesNotWaitForBlockedEnqueue(t *testing.T) {
	conn, node, _ := newHandoffConnection(t)
	entered, sent, updated := make(chan struct{}), make(chan struct{}), make(chan struct{})
	releaseSignal, release := newHandoffRelease(t)
	node.beforeSend = func(*relay.Package) error { close(entered); <-releaseSignal; return nil }
	var sendErr error
	go func() {
		defer close(sent)
		sendErr = conn.forwardMessageToRelay(websocket.MessageText, []byte("blocked"))
	}()
	waitHandoff(t, entered)
	go func() {
		defer close(updated)
		conn.handleControlMessage(payload.NewPayload([]byte(`{"metadata":{"user_id":"alice"},"message_topic":"custom.message"}`), payload.JSON))
	}()
	select {
	case <-updated:
	case <-time.After(3 * time.Second):
		release()
		waitHandoff(t, sent)
		t.Fatal("state-only control waited on relay backpressure")
	}
	release()
	waitHandoff(t, sent)
	require.NoError(t, sendErr)
	require.Equal(t, "alice", conn.config.Metadata["user_id"])
	require.Equal(t, "custom.message", conn.currentMessageTopic)
}

func TestHandoff_ControlUpdatesPreserveOptionalFields(t *testing.T) {
	conn, node, _ := newHandoffConnection(t)
	conn.currentMessageTopic = "custom.message"
	conn.config.Metadata = map[string]any{"user_id": "alice"}
	conn.handleControlMessage(payload.NewPayload([]byte(`{"target_pid":"`+handoffAddress("new")+`"}`), payload.JSON))
	require.NoError(t, conn.forwardMessageToRelay(websocket.MessageText, []byte("after")))
	delivered := node.snapshot()
	require.Len(t, delivered, 3)
	require.Equal(t, "alice", delivered[1].join.Metadata["user_id"])
	require.Equal(t, "custom.message", delivered[2].topic)
	require.Equal(t, handoffPID("new"), delivered[2].target)
}

func BenchmarkForwardMessageToRelay(b *testing.B) {
	node := &wsRecordingNode{}
	conn := &Connection{ctx: context.Background(), node: node, logger: zap.NewNop(), currentTargetPID: handoffPID("hub"), wsPID: handoffPID("client"), currentMessageTopic: MessageTopic}
	data := []byte(`{"type":"probe_action"}`)
	b.ReportAllocs()
	for b.Loop() {
		if err := conn.forwardMessageToRelay(websocket.MessageText, data); err != nil {
			b.Fatal(err)
		}
	}
}
