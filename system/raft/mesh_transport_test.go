// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/cluster/internode"
)

// yamuxTestConfig mirrors the production session config so test sessions
// behave like the real mesh ones.
func yamuxTestConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = io.Discard
	cfg.EnableKeepAlive = false
	return cfg
}

// pairedConnMgr is a fake ConnectionManager that ties two endpoints
// together so each side's SendToNode dispatches into the other side's
// registered class receiver. There is no real TCP — frames arrive
// in-order, synchronously, into the peer's inbox.
type pairedConnMgr struct {
	peerMgr   *pairedConnMgr
	receivers map[internode.Class]func(cluster.NodeID, []byte)
	overflow  map[internode.Class]func(cluster.NodeID)
	self      cluster.NodeID
	mu        sync.Mutex
}

func newPairedPair(a, b cluster.NodeID) (*pairedConnMgr, *pairedConnMgr) {
	pa := &pairedConnMgr{
		self:      a,
		receivers: map[internode.Class]func(cluster.NodeID, []byte){},
		overflow:  map[internode.Class]func(cluster.NodeID){},
	}
	pb := &pairedConnMgr{
		self:      b,
		receivers: map[internode.Class]func(cluster.NodeID, []byte){},
		overflow:  map[internode.Class]func(cluster.NodeID){},
	}
	pa.peerMgr = pb
	pb.peerMgr = pa
	return pa, pb
}

func (p *pairedConnMgr) Start(_ context.Context, _ func(cluster.NodeID, []byte)) error {
	return nil
}
func (p *pairedConnMgr) Stop() error { return nil }

func (p *pairedConnMgr) SendToNode(_ cluster.NodeID, data []byte, class internode.Class) error {
	p.peerMgr.mu.Lock()
	r := p.peerMgr.receivers[class]
	p.peerMgr.mu.Unlock()
	if r == nil {
		return errors.New("no receiver")
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	r(p.self, cp)
	return nil
}

func (p *pairedConnMgr) EnsureConnection(_ cluster.NodeID, _ string, _ int) {}
func (p *pairedConnMgr) DisconnectFromNode(_ cluster.NodeID)                {}
func (p *pairedConnMgr) ConnectedNodes() []cluster.NodeID                   { return nil }
func (p *pairedConnMgr) GetListenPort() int                                 { return 0 }
func (p *pairedConnMgr) AddManagedNode(_ cluster.NodeID)                    {}
func (p *pairedConnMgr) RemoveManagedNode(_ cluster.NodeID)                 {}
func (p *pairedConnMgr) IsManaged(_ cluster.NodeID) bool                    { return true }
func (p *pairedConnMgr) EvictOrphanNodes(_ map[cluster.NodeID]struct{}) int { return 0 }
func (p *pairedConnMgr) RecordDropReason(_ string)                          {}

func (p *pairedConnMgr) RegisterClassReceiver(class internode.Class, recv func(cluster.NodeID, []byte)) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if recv != nil && p.receivers[class] != nil {
		return false
	}
	p.receivers[class] = recv
	return true
}

func (p *pairedConnMgr) RegisterClassOverflowHandler(class internode.Class, handler func(cluster.NodeID)) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if handler != nil && p.overflow[class] != nil {
		return false
	}
	p.overflow[class] = handler
	return true
}

// fireOverflow invokes the registered overflow handler for class as the
// connection manager would on a send-queue overflow. nodeID is the peer
// whose session must be reset.
func (p *pairedConnMgr) fireOverflow(class internode.Class, nodeID cluster.NodeID) {
	p.mu.Lock()
	h := p.overflow[class]
	p.mu.Unlock()
	if h != nil {
		h(nodeID)
	}
}

var _ internode.ConnectionManager = (*pairedConnMgr)(nil)

// TestMeshTransport_RoundTripAppendEntries opens a yamux stream across
// two paired stream layers, writes a payload that mirrors a Raft
// AppendEntries body, and asserts every byte arrives unchanged on the
// other side. The mesh fabric is otherwise opaque to hashicorp/raft;
// proving byte-equivalence is sufficient to know its NetworkTransport
// would deliver the framed AppendEntries to the FSM.
func TestMeshTransport_RoundTripAppendEntries(t *testing.T) {
	logger := zap.NewNop()
	mgrA, mgrB := newPairedPair("node-A", "node-B")

	layerA := newMeshStreamLayer("node-A", mgrA, logger)
	layerB := newMeshStreamLayer("node-B", mgrB, logger)
	t.Cleanup(func() {
		_ = layerA.Close()
		_ = layerB.Close()
	})
	require.NoError(t, layerA.register())
	require.NoError(t, layerB.register())

	type seenStream struct {
		conn net.Conn
		err  error
	}
	acceptCh := make(chan seenStream, 1)
	go func() {
		c, err := layerB.Accept()
		acceptCh <- seenStream{conn: c, err: err}
	}()

	stream, err := layerA.Dial("node-B", 2*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	payload := []byte("AppendEntries{term=7,leader=node-A,entries=12}")
	go func() {
		_, _ = stream.Write(payload)
	}()

	var got seenStream
	select {
	case got = <-acceptCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server side never accepted the stream")
	}
	require.NoError(t, got.err)
	require.NotNil(t, got.conn)
	t.Cleanup(func() { _ = got.conn.Close() })

	buf := make([]byte, len(payload))
	require.NoError(t, got.conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	n, err := got.conn.Read(buf)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	require.Equal(t, payload, buf[:n])
}

// TestMeshTransport_AcceptUnblocksOnClose proves the Accept() contract:
// it blocks while no stream is offered, returns a typed error when the
// stream layer is closed, and does not leak the calling goroutine on
// shutdown.
func TestMeshTransport_AcceptUnblocksOnClose(t *testing.T) {
	mgr, _ := newPairedPair("node-A", "node-B")
	layer := newMeshStreamLayer("node-A", mgr, zap.NewNop())
	require.NoError(t, layer.register())

	done := make(chan error, 1)
	go func() {
		_, err := layer.Accept()
		done <- err
	}()

	// Accept must be blocking before any stream arrives or Close fires.
	select {
	case err := <-done:
		t.Fatalf("Accept returned prematurely: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	require.NoError(t, layer.Close())

	select {
	case err := <-done:
		require.ErrorIs(t, err, net.ErrClosed)
	case <-time.After(time.Second):
		t.Fatal("Accept did not unblock after Close")
	}
}

// TestMeshTransport_DialUnknownPeerTimesOut asserts Dial returns within
// the supplied timeout when the peer never responds (no inbound frames
// ever flow back through onInbound). Important because hraft uses the
// returned error to decide whether to retry; a forever-hung Dial would
// stall the election timer.
func TestMeshTransport_DialUnknownPeerTimesOut(t *testing.T) {
	// Isolated manager: no paired peer means SendToNode succeeds but no
	// inbound traffic ever arrives, so the yamux SYN handshake never
	// completes.
	mgr := &pairedConnMgr{
		self:      "node-A",
		receivers: map[internode.Class]func(cluster.NodeID, []byte){},
		overflow:  map[internode.Class]func(cluster.NodeID){},
	}
	mgr.peerMgr = &pairedConnMgr{
		self:      "node-B",
		receivers: map[internode.Class]func(cluster.NodeID, []byte){},
		overflow:  map[internode.Class]func(cluster.NodeID){},
	}
	layer := newMeshStreamLayer("node-A", mgr, zap.NewNop())
	t.Cleanup(func() { _ = layer.Close() })
	require.NoError(t, layer.register())

	start := time.Now()
	_, err := layer.Dial("node-B", 200*time.Millisecond)
	require.Error(t, err)
	require.Less(t, time.Since(start), 2*time.Second,
		"Dial must honor the timeout argument")
}

// TestMeshTransport_RemovePeerTearsDownSession proves the NodeLeft
// teardown: a peer session created on first use is fully removed from
// the sessions map and closed when the peer departs, and a subsequent
// getOrCreateSession builds a FRESH session (new pointer) rather than
// reusing the stale one. Without removePeer, the stale session lingers
// forever (leak) and rejoin reuses it against the wrong incarnation.
func TestMeshTransport_RemovePeerTearsDownSession(t *testing.T) {
	mgrA, _ := newPairedPair("node-A", "node-B")
	layer := newMeshStreamLayer("node-A", mgrA, zap.NewNop())
	t.Cleanup(func() { _ = layer.Close() })
	require.NoError(t, layer.register())

	old := layer.getOrCreateSession("node-B")
	require.NotNil(t, old)

	layer.mu.Lock()
	_, present := layer.sessions["node-B"]
	layer.mu.Unlock()
	require.True(t, present, "session must exist before teardown")

	layer.removePeer("node-B")

	layer.mu.Lock()
	_, present = layer.sessions["node-B"]
	layer.mu.Unlock()
	require.False(t, present, "session must be removed after teardown")

	// The old session's underlying transport must be closed so the
	// acceptLoop goroutine exits.
	require.True(t, old.session.IsClosed(), "old yamux session must be closed")
	require.True(t, old.conn.closed.Load(), "old classConn must be closed")

	// acceptLoop for the torn-down session must terminate.
	require.Eventually(t, func() bool {
		_, err := old.session.AcceptStream()
		return err != nil
	}, time.Second, 5*time.Millisecond, "old session must reject accepts")

	fresh := layer.getOrCreateSession("node-B")
	require.NotNil(t, fresh)
	require.NotSame(t, old, fresh, "rejoin must build a fresh session, not reuse the stale one")
}

// TestMeshTransport_OverflowResetsSession proves the byte-stream reset
// contract end-to-end through the mesh layer: when the connection manager
// reports a ClassRaftMesh send-queue overflow (which it does instead of
// dropping a frame from the middle of the yamux stream), the registered
// overflow handler tears down the peer's session so yamux+raft rebuild a
// fresh one. The fresh session is a new pointer; the old one is closed.
func TestMeshTransport_OverflowResetsSession(t *testing.T) {
	mgrA, _ := newPairedPair("node-A", "node-B")
	layer := newMeshStreamLayer("node-A", mgrA, zap.NewNop())
	t.Cleanup(func() { _ = layer.Close() })
	require.NoError(t, layer.register())

	old := layer.getOrCreateSession("node-B")
	require.NotNil(t, old)

	// The connection manager invokes the registered overflow handler for
	// the peer whose ClassRaftMesh queue overflowed.
	mgrA.fireOverflow(internode.ClassRaftMesh, "node-B")

	// The stale session is gone and its transport is closed so the
	// acceptLoop goroutine exits. A mid-stream drop would instead have
	// left the session in place but silently corrupted; the contract is a
	// clean teardown.
	layer.mu.Lock()
	_, present := layer.sessions["node-B"]
	layer.mu.Unlock()
	require.False(t, present, "overflow must tear down the peer session")
	require.True(t, old.session.IsClosed(), "old yamux session must be closed")
	require.True(t, old.conn.closed.Load(), "old classConn must be closed")

	// A subsequent use rebuilds a fresh session rather than reusing stale.
	fresh := layer.getOrCreateSession("node-B")
	require.NotNil(t, fresh)
	require.NotSame(t, old, fresh, "reset must build a fresh session")
}

// TestMeshTransport_InboundOverflowResetsSession proves the RX-side
// byte-stream contract: when the per-peer inbound buffer is full, a frame
// must NOT be dropped (a mid-stream gap desyncs the yamux demuxer just
// like a dropped outbound frame). Instead the session is reset so it
// rebuilds and resyncs. The test fills the inbound buffer past capacity
// and asserts the session is torn down.
func TestMeshTransport_InboundOverflowResetsSession(t *testing.T) {
	mgrA, _ := newPairedPair("node-A", "node-B")
	layer := newMeshStreamLayer("node-A", mgrA, zap.NewNop())
	t.Cleanup(func() { _ = layer.Close() })
	require.NoError(t, layer.register())

	// Install a synthetic peer session whose classConn has no yamux recv
	// loop draining its inbound buffer. (A live yamux session would race
	// to drain c.incoming, making saturation non-deterministic.) The yamux
	// session is built over a separate dead pipe so removePeer can close
	// it without affecting the saturated conn.
	conn := newClassConn("node-A", "node-B", mgrA)
	deadA, deadB := net.Pipe()
	t.Cleanup(func() { _ = deadB.Close() })
	cfg := yamuxTestConfig()
	ys, err := yamux.Client(deadA, cfg)
	require.NoError(t, err)
	sess := &peerSession{conn: conn, session: ys}

	layer.mu.Lock()
	layer.sessions["node-B"] = sess
	layer.mu.Unlock()

	// Saturate the inbound channel to capacity; nothing drains it.
	bufCap := cap(conn.incoming)
	for i := 0; i < bufCap; i++ {
		layer.onInbound("node-B", []byte{byte(i)})
	}
	// Buffer is full; the next frame overflows and triggers a reset.
	layer.onInbound("node-B", []byte{0xFF})

	layer.mu.Lock()
	_, present := layer.sessions["node-B"]
	layer.mu.Unlock()
	require.False(t, present, "inbound overflow must tear down the peer session")
	require.True(t, conn.closed.Load(), "old classConn must be closed after inbound overflow")
}

// TestClassConn_InjectInbound_DeliversAndReportsDrop asserts injectInbound
// returns true while the buffer has room and false on overflow, so the
// caller can decide to reset rather than silently drop a stream frame.
func TestClassConn_InjectInbound_DeliversAndReportsDrop(t *testing.T) {
	mgrA, _ := newPairedPair("node-A", "node-B")
	c := newClassConn("node-A", "node-B", mgrA)

	for i := 0; i < cap(c.incoming); i++ {
		require.True(t, c.injectInbound([]byte{byte(i)}), "delivery must succeed while buffered")
	}
	require.False(t, c.injectInbound([]byte{0xFF}), "overflow must report a drop")

	// A closed conn reports delivered=true (stream gone, no reset needed).
	require.NoError(t, c.Close())
	require.True(t, c.injectInbound([]byte{0x01}), "closed conn must not request a reset")
}

// TestMeshTransport_RegisterClaimsOverflowHandler asserts register wires
// both the inbound receiver and the overflow handler, and that a second
// register on a manager that already has the class fails.
func TestMeshTransport_RegisterClaimsOverflowHandler(t *testing.T) {
	mgrA, _ := newPairedPair("node-A", "node-B")
	layer := newMeshStreamLayer("node-A", mgrA, zap.NewNop())
	t.Cleanup(func() { _ = layer.Close() })
	require.NoError(t, layer.register())

	mgrA.mu.Lock()
	_, hasRecv := mgrA.receivers[internode.ClassRaftMesh]
	_, hasOverflow := mgrA.overflow[internode.ClassRaftMesh]
	mgrA.mu.Unlock()
	require.True(t, hasRecv, "register must claim the inbound receiver")
	require.True(t, hasOverflow, "register must claim the overflow handler")

	second := newMeshStreamLayer("node-A", mgrA, zap.NewNop())
	require.Error(t, second.register(), "second register on a claimed manager must fail")
}

// TestMeshTransport_RemovePeerIdempotent asserts the teardown is a no-op
// for an unknown / already-removed peer.
func TestMeshTransport_RemovePeerIdempotent(t *testing.T) {
	mgrA, _ := newPairedPair("node-A", "node-B")
	layer := newMeshStreamLayer("node-A", mgrA, zap.NewNop())
	t.Cleanup(func() { _ = layer.Close() })
	require.NoError(t, layer.register())

	require.NotPanics(t, func() {
		layer.removePeer("node-unknown")
		layer.removePeer("node-B")
		layer.removePeer("node-B")
	})
}

// TestMeshTransport_InboundAfterCloseDoesNotPanic guards a regression
// observed in the chaos rig: after Close() niled the sessions map,
// late ClassRaftMesh frames still arriving from the connection manager
// would race into getOrCreateSession and panic with
// "assignment to entry in nil map". The post-fix contract: late
// inbound frames are dropped silently and the goroutine returns.
func TestMeshTransport_InboundAfterCloseDoesNotPanic(t *testing.T) {
	mgrA, _ := newPairedPair("node-A", "node-B")
	layer := newMeshStreamLayer("node-A", mgrA, zap.NewNop())
	require.NoError(t, layer.register())

	require.NoError(t, layer.Close())

	require.NotPanics(t, func() {
		layer.onInbound("node-B", []byte{0x01, 0x02, 0x03})
	})
}
