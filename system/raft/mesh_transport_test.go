// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/cluster/internode"
)

// pairedConnMgr is a fake ConnectionManager that ties two endpoints
// together so each side's SendToNode dispatches into the other side's
// registered class receiver. There is no real TCP — frames arrive
// in-order, synchronously, into the peer's inbox.
type pairedConnMgr struct {
	peerMgr   *pairedConnMgr
	receivers map[internode.Class]func(cluster.NodeID, []byte)
	self      cluster.NodeID
	mu        sync.Mutex
}

func newPairedPair(a, b cluster.NodeID) (*pairedConnMgr, *pairedConnMgr) {
	pa := &pairedConnMgr{self: a, receivers: map[internode.Class]func(cluster.NodeID, []byte){}}
	pb := &pairedConnMgr{self: b, receivers: map[internode.Class]func(cluster.NodeID, []byte){}}
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
	}
	mgr.peerMgr = &pairedConnMgr{
		self:      "node-B",
		receivers: map[internode.Class]func(cluster.NodeID, []byte){},
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
