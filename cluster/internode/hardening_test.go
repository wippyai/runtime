// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

// endRecorder records session-end signals in order.
type endRecorder struct {
	ends []cluster.NodeID
	mu   sync.Mutex
}

func (r *endRecorder) record(id cluster.NodeID) {
	r.mu.Lock()
	r.ends = append(r.ends, id)
	r.mu.Unlock()
}

func (r *endRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ends)
}

// liveReader binds a connection to state's current session and registers it
// as that session's live reader, as a Run blocked inside a handler would.
func liveReader(nsm *NodeStateManager, nodeID cluster.NodeID, state *NodeState) *NodeConnection {
	conn := &NodeConnection{}
	conn.bindSession(nsm, nodeID, state, 32)
	if !conn.link.beginRead() {
		panic("session already ended")
	}
	return conn
}

func requireOpen(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal(msg)
	default:
	}
}

// A session whose reader is still inside a handler has not been signaled
// ended. Every later end for the node — a restart of the readmitted node, a
// departure with no state — waits for it: no end signal runs and no later
// session is released before the earlier end is signaled.
func TestSessionEndSignalsAreChained(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.Logger = zap.NewNop()
	m := NewConnectionManager(cfg, nil).(*manager)
	rec := &endRecorder{}
	m.nodeStates.sessionEnded = rec.record

	m.AddManagedNode("peer")
	first := m.nodeStates.GetNodeState("peer")
	require.Equal(t, incarnationBound, m.nodeStates.bindPeerIncarnation("peer", first, 1))
	stuck := liveReader(m.nodeStates, "peer", first)

	// The node departs while the first session's reader is inside a handler.
	m.RemoveManagedNode("peer", 0)
	require.Zero(t, rec.count())

	// The node is readmitted and restarts: the readmitted state's session
	// ends with no live reader.
	m.AddManagedNode("peer")
	second := m.nodeStates.GetNodeState("peer")
	require.Equal(t, incarnationBound, m.nodeStates.bindPeerIncarnation("peer", second, 1))
	require.Equal(t, incarnationRestarted, m.nodeStates.bindPeerIncarnation("peer", second, 2))
	second.queueMu.Lock()
	restarted := second.session
	second.queueMu.Unlock()
	require.Zero(t, rec.count(), "a later end was signaled before the pending one")
	requireOpen(t, restarted.predecessor, "the restarted session was released before the pending end")

	// The readmitted node departs, then departs again with no state behind
	// it; both wait.
	m.RemoveManagedNode("peer", 0)
	m.RemoveManagedNode("peer", 0)
	m.AddManagedNode("peer")
	third := m.nodeStates.GetNodeState("peer")
	third.queueMu.Lock()
	readmitted := third.session
	third.queueMu.Unlock()
	require.NotNil(t, readmitted.predecessor)
	requireOpen(t, readmitted.predecessor, "the readmitted session was released before the pending end")
	require.Zero(t, rec.count())

	// The stuck reader leaves its handler: every end is signaled in order and
	// the latest session is released.
	stuck.link.endRead()
	require.Eventually(t, func() bool {
		select {
		case <-readmitted.predecessor:
			return true
		default:
			return false
		}
	}, 2*time.Second, time.Millisecond)
	// The first departure, the restart, the readmitted node's departure, and
	// the departure with no state.
	require.Equal(t, 4, rec.count())
}

// A handshaken connection that cannot be handed to a live control loop is
// closed.
func TestConnectionForUnmanagedNodeIsClosed(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.LocalNodeID = "a-local"
	cfg.BindAddr = "127.0.0.1"
	cfg.BindPort = 0
	cfg.Logger = zap.NewNop()
	m := NewConnectionManager(cfg, nil).(*manager)
	require.NoError(t, m.Start(context.Background(), func(cluster.NodeID, []byte) {}, ignoreSessionEnd))
	defer func() { require.NoError(t, m.Stop()) }()

	local, remote := net.Pipe()
	defer remote.Close()
	conn := newNodeConnection(local, "z-peer", testIncarnation, cfg.NodeConnectionConfig(), zap.NewNop())
	m.sendCommand("z-peer", nodeCommand{Type: cmdConnected, Data: connectedData{Connection: conn}})
	require.True(t, conn.closed.Load(), "a connection for an unmanaged node leaked")
}

// fakeDialPeer accepts the manager's dials and plays the plain handshake as
// the peer. hold, when set, delays the peer's half of the handshake; resume
// controls whether the peer answers RESUME.
type fakeDialPeer struct {
	listener net.Listener
	conns    chan net.Conn
	hold     chan struct{}
	id       cluster.NodeID
}

func startFakeDialPeer(t *testing.T, id cluster.NodeID, hold chan struct{}) *fakeDialPeer {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	p := &fakeDialPeer{listener: listener, conns: make(chan net.Conn, 16), hold: hold, id: id}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
			go func() {
				if _, err := readPlainEndpoint(conn); err != nil {
					return
				}
				if p.hold != nil {
					<-p.hold
				}
				if err := writePlainEndpoint(conn, handshakeEndpoint{id: id, incarnation: testIncarnation}); err != nil {
					return
				}
				p.conns <- conn
			}()
		}
	}()
	return p
}

func (p *fakeDialPeer) port() int { return p.listener.Addr().(*net.TCPAddr).Port }

// requireClosedByManager waits until the manager closes its end of conn.
func requireClosedByManager(t *testing.T, conn net.Conn, within time.Duration) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(within)))
	buf := make([]byte, 4096)
	for {
		_, err := conn.Read(buf)
		if err == nil {
			continue
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			t.Fatal("the manager kept the connection open")
		}
		require.True(t, errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || isConnReset(err), "unexpected read error: %v", err)
		return
	}
}

func isConnReset(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "read"
}

// A dial whose handshake completes after the node's control loop ended does
// not leak its connection.
func TestDialCompletingAfterLoopEndsIsClosed(t *testing.T) {
	for range 10 {
		func() {
			cfg := insecureManagerConfig()
			cfg.LocalNodeID = "a-local"
			cfg.BindAddr = "127.0.0.1"
			cfg.BindPort = 0
			cfg.Logger = zap.NewNop()
			m := NewConnectionManager(cfg, nil).(*manager)
			require.NoError(t, m.Start(context.Background(), func(cluster.NodeID, []byte) {}, ignoreSessionEnd))
			defer func() { require.NoError(t, m.Stop()) }()

			hold := make(chan struct{})
			peer := startFakeDialPeer(t, "z-peer", hold)
			m.AddManagedNode("z-peer")
			m.EnsureConnection("z-peer", "127.0.0.1", peer.port())
			// Wait until the dial reached the held handshake, then end the
			// loop before the handshake completes.
			time.Sleep(20 * time.Millisecond)
			m.RemoveManagedNode("z-peer", 0)
			close(hold)
			conn := <-peer.conns
			requireClosedByManager(t, conn, 2*time.Second)
		}()
	}
}

// A peer that completes the handshake but never sends RESUME does not hold
// the link: the connection times out and the manager dials again.
func TestMissingResumeTimesOutAndRedials(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.LocalNodeID = "a-local"
	cfg.BindAddr = "127.0.0.1"
	cfg.BindPort = 0
	cfg.Logger = zap.NewNop()
	cfg.HandshakeTimeout = 200 * time.Millisecond
	cfg.InitialRetryDelay = time.Millisecond
	cfg.MaxRetryDelay = 10 * time.Millisecond
	m := NewConnectionManager(cfg, nil).(*manager)
	require.NoError(t, m.Start(context.Background(), func(cluster.NodeID, []byte) {}, ignoreSessionEnd))
	defer func() { require.NoError(t, m.Stop()) }()

	peer := startFakeDialPeer(t, "z-peer", nil)
	m.AddManagedNode("z-peer")
	m.EnsureConnection("z-peer", "127.0.0.1", peer.port())
	first := <-peer.conns
	requireClosedByManager(t, first, 3*time.Second)
	select {
	case <-peer.conns:
	case <-time.After(3 * time.Second):
		t.Fatal("the manager did not dial again")
	}
}

// acceptFromIncarnation feeds m an inbound connection from peer claiming the
// given incarnation and returns the peer's end.
func acceptFromIncarnation(t *testing.T, m *manager, peer cluster.NodeID, incarnation uint64) *NodeConnection {
	t.Helper()
	server, client := net.Pipe()
	go m.handleInboundConnection(server)
	remote, err := PerformClientHandshake(client, m.config.NodeConnectionConfig(), zap.NewNop(), peer, incarnation, m.config.LocalNodeID)
	require.NoError(t, err)
	t.Cleanup(remote.Close)
	return remote
}

// A late connection from a node's previous process cannot displace the
// session of its current process: membership names the current incarnation.
func TestStaleIncarnationCannotDisplaceNewer(t *testing.T) {
	const peer, oldInc, newInc = "z-peer", uint64(11), uint64(22)
	cfg := insecureManagerConfig()
	cfg.LocalNodeID = "a-local"
	cfg.BindAddr = "127.0.0.1"
	cfg.BindPort = 0
	cfg.Logger = zap.NewNop()
	advertised := map[cluster.NodeID]uint64{peer: newInc}
	authorizeAdvertisedIncarnation(&cfg, advertised)
	m := NewConnectionManager(cfg, nil).(*manager)
	rec := &endRecorder{}
	require.NoError(t, m.Start(context.Background(), func(cluster.NodeID, []byte) {}, rec.record))
	defer func() { require.NoError(t, m.Stop()) }()
	m.AddManagedNode(peer)

	acceptFromIncarnation(t, m, peer, newInc)
	require.Eventually(t, func() bool {
		_, state := m.nodeStates.GetNodeConnection(peer)
		return state == StateConnected
	}, 2*time.Second, time.Millisecond)
	current, _ := m.nodeStates.GetNodeConnection(peer)

	stale := acceptFromIncarnation(t, m, peer, oldInc)
	requireClosedByPeer(t, stale)
	state := m.nodeStates.GetNodeState(peer)
	state.queueMu.Lock()
	bound := state.session.peerIncarnation
	state.queueMu.Unlock()
	require.Equal(t, newInc, bound)
	conn, connState := m.nodeStates.GetNodeConnection(peer)
	require.Same(t, current, conn)
	require.Equal(t, StateConnected, connState)
	require.Zero(t, rec.count(), "a stale connection signaled the current session down")
}

// A successor replaced by a preferred connection is closed.
func TestReplacedSuccessorIsClosed(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.LocalNodeID = "a-local"
	cfg.Logger = zap.NewNop()
	m := NewConnectionManager(cfg, nil).(*manager)
	m.nodeStates.sessionEnded = ignoreSessionEnd
	m.AddManagedNode("z-peer")
	state := m.nodeStates.GetNodeState("z-peer")
	pipeConn := func(dialed bool, incarnation uint64) *NodeConnection {
		a, b := net.Pipe()
		t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
		c := newNodeConnection(a, "z-peer", incarnation, cfg.NodeConnectionConfig(), zap.NewNop())
		c.dialed = dialed
		return c
	}
	current := pipeConn(true, 1)
	require.Equal(t, incarnationBound, m.nodeStates.bindPeerIncarnation("z-peer", state, 1))
	loop := &nodeControlLoop{manager: m, nodeID: "z-peer", nodeState: state, connection: current, state: StateConnected, logger: zap.NewNop()}

	// The peer restarted: its connection becomes the successor whatever its
	// direction.
	restarted := pipeConn(false, 2)
	loop.handleConnected(connectedData{Connection: restarted})
	require.Same(t, restarted, loop.successor)

	// A preferred connection of the same process replaces it.
	preferred := pipeConn(true, 2)
	loop.handleConnected(connectedData{Connection: preferred})
	require.Same(t, preferred, loop.successor)
	require.True(t, restarted.closed.Load(), "the replaced successor leaked")
}

// Creating state for a node that already has it keeps the existing state.
func TestCreateNodeStateKeepsExistingState(t *testing.T) {
	nsm := setupStateManager()
	nsm.CreateNodeState("peer")
	state := nsm.GetNodeState("peer")
	require.NoError(t, nsm.QueueMessageClass("peer", []byte("queued"), ClassRaftControl))
	state.queueMu.Lock()
	sess := state.session
	state.queueMu.Unlock()

	nsm.CreateNodeState("peer")
	require.Same(t, state, nsm.GetNodeState("peer"))
	state.queueMu.Lock()
	require.Same(t, sess, state.session)
	state.queueMu.Unlock()
	require.Equal(t, [][]byte{[]byte("queued")}, drainAllData(nsm, "peer"))
}

// authorizeAdvertisedIncarnation makes cfg accept only the incarnation
// membership advertises for each node.
func authorizeAdvertisedIncarnation(cfg *ManagerConfig, advertised map[cluster.NodeID]uint64) {
	membership := &mockMembership{}
	for id, incarnation := range advertised {
		membership.nodes = append(membership.nodes, cluster.NodeInfo{ID: id, Meta: cluster.NodeMeta{
			cluster.MetaIncarnation: strconv.FormatUint(incarnation, 10),
		}})
	}
	cfg.AuthorizeIncarnation = func(id cluster.NodeID, incarnation uint64) bool {
		return MemberIncarnationAdvertised(membership, id, incarnation)
	}
}
