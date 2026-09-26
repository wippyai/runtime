// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

// Pause inbound authorization after its initial missing-state check. A formal
// membership join then creates the state and admits a message before inbound
// auto-management resumes. The duplicate join must not reset that queue, so
// the adopted inbound connection delivers the admitted message.
func TestInboundAutoManagementPreservesConcurrentMembershipJoin(t *testing.T) {
	observed := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	defer resume()
	cfg := insecureManagerConfig()
	cfg.LocalNodeID = "a-local"
	cfg.BindAddr = "127.0.0.1"
	cfg.BindPort = 0
	cfg.Logger = zap.NewNop()
	cfg.AuthorizePeer = func(cluster.NodeID, net.Addr) bool { close(observed); <-release; return true }
	m := NewConnectionManager(cfg, nil).(*manager)
	require.NoError(t, m.Start(context.Background(), func(cluster.NodeID, []byte) {}, ignoreSessionEnd))
	defer func() { require.NoError(t, m.Stop()) }()
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	finished := make(chan struct{})
	go func() { defer close(finished); m.handleInboundConnection(server) }()
	peer, err := PerformClientHandshake(client, cfg.NodeConnectionConfig(), zap.NewNop(), "z-peer", testIncarnation, "a-local")
	require.NoError(t, err)
	defer peer.Close()
	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Fatal("inbound admission not reached")
	}
	m.AddManagedNode("z-peer")
	original := m.nodeStates.GetNodeState("z-peer")
	require.NotNil(t, original)
	require.NoError(t, m.nodeStates.QueueMessageClass("z-peer", []byte("already-admitted"), ClassRaftRPC))
	resume()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("inbound handler did not finish")
	}
	require.Same(t, original, m.nodeStates.GetNodeState("z-peer"))
	require.NoError(t, peer.conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	answerResume(t, peer, cfg.MaxMessageSize)
	f, err := readFrame(peer.conn, cfg.MaxMessageSize)
	require.NoError(t, err)
	require.Equal(t, ClassRaftRPC, f.class)
	require.Equal(t, []byte("already-admitted"), f.data)
	m.RemoveManagedNode("z-peer", 0)
}

func TestConcurrentManagedJoinsPreserveAdmittedMessages(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.Logger = zap.NewNop()
	m := NewConnectionManager(cfg, nil).(*manager)
	start := make(chan struct{})
	var workers sync.WaitGroup
	failures := make(chan error, 32)
	for i := range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			m.AddManagedNode("peer")
			failures <- m.nodeStates.QueueMessageClass("peer", []byte{byte(i)}, ClassRaftRPC)
		}()
	}
	close(start)
	workers.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	queued := drainAllData(m.nodeStates, "peer")
	require.Len(t, queued, 32)
	seen := make(map[byte]bool)
	for _, data := range queued {
		require.Len(t, data, 1)
		require.False(t, seen[data[0]])
		seen[data[0]] = true
	}
	m.RemoveManagedNode("peer", 0)
}

func TestDetachedPeerCleanupDoesNotRemoveReplacement(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.Logger = zap.NewNop()
	m := NewConnectionManager(cfg, nil).(*manager)
	m.AddManagedNode("peer")
	old := m.nodeStates.GetNodeState("peer")
	old.stateMu.Lock()
	var once sync.Once
	release := func() { once.Do(old.stateMu.Unlock) }
	defer release()
	removed := make(chan struct{})
	go func() { m.RemoveManagedNode("peer", 0); close(removed) }()
	require.Eventually(t, func() bool { return m.nodeStates.GetNodeState("peer") == nil }, time.Second, time.Millisecond)
	joined := make(chan struct{})
	go func() { m.AddManagedNode("peer"); close(joined) }()
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("old resource cleanup blocked new admission")
	}
	replacement := m.nodeStates.GetNodeState("peer")
	require.NotSame(t, old, replacement)
	require.NoError(t, m.nodeStates.QueueMessageClass("peer", []byte("new-generation"), ClassRaftRPC))
	release()
	select {
	case <-removed:
	case <-time.After(time.Second):
		t.Fatal("old cleanup did not finish")
	}
	require.Same(t, replacement, m.nodeStates.GetNodeState("peer"))
	require.Equal(t, [][]byte{[]byte("new-generation")}, drainAllData(m.nodeStates, "peer"))
	m.RemoveManagedNode("peer", 0)
}

// A cancelled loop can run its deferred cleanup after membership removal has
// detached its state and a new incarnation has been admitted. That cleanup
// must remain scoped to the generation the loop originally owned.
func TestDetachedLoopCleanupDoesNotMutateReplacementGeneration(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.Logger = zap.NewNop()
	m := NewConnectionManager(cfg, nil).(*manager)
	m.AddManagedNode("peer")
	old := m.nodeStates.GetNodeState("peer")
	require.NotNil(t, old)

	ctx, cancel := context.WithCancel(context.Background())
	oldLoop := &nodeControlLoop{
		ctx:       ctx,
		cancel:    cancel,
		manager:   m,
		nodeID:    "peer",
		nodeState: old,
		commands:  make(chan nodeCommand),
	}
	m.controlLoopsMu.Lock()
	m.controlLoops["peer"] = oldLoop
	m.controlLoopsMu.Unlock()

	m.RemoveManagedNode("peer", 0)
	select {
	case <-ctx.Done():
	default:
		t.Fatal("remove did not cancel the old control loop")
	}
	m.AddManagedNode("peer")
	replacement := m.nodeStates.GetNodeState("peer")
	require.NotNil(t, replacement)
	require.NotSame(t, old, replacement)
	m.nodeStates.SetNodeState("peer", StateRetrying)

	// This is the deferred cleanup the cancelled old loop runs after Remove.
	oldLoop.cleanup()
	_, got := m.nodeStates.GetNodeConnection("peer")
	require.Equal(t, StateRetrying, got, "old-loop cleanup changed replacement state")
	m.RemoveManagedNode("peer", 0)
}

func TestDetachedLoopDrainDoesNotTouchReplacementGeneration(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.Logger = zap.NewNop()
	m := NewConnectionManager(cfg, nil).(*manager)
	m.AddManagedNode("peer")
	old := m.nodeStates.GetNodeState("peer")
	require.NotNil(t, old)

	conn := &NodeConnection{}
	conn.bindSession(m.nodeStates, "peer", old, 32)

	m.RemoveManagedNode("peer", 0)
	m.AddManagedNode("peer")
	replacement := m.nodeStates.GetNodeState("peer")
	require.NotNil(t, replacement)
	require.NotSame(t, old, replacement)
	require.NoError(t, m.nodeStates.QueueMessageClass("peer", []byte("replacement"), ClassRaftRPC))

	require.Empty(t, m.nodeStates.drainSession("peer", conn.link.state, conn.link.sess, 1), "stale drain consumed replacement data")
	require.Equal(t, [][]byte{[]byte("replacement")}, drainAllData(m.nodeStates, "peer"))
	m.RemoveManagedNode("peer", 0)
}

// answerResume plays the peer side of session agreement on a raw connection:
// it reads the manager's RESUME and answers as a fresh session that has
// received nothing.
func answerResume(t *testing.T, peer *NodeConnection, maxMessageSize uint32) {
	t.Helper()
	f, err := readFrame(peer.conn, maxMessageSize)
	require.NoError(t, err)
	require.Equal(t, classResume, f.class)
	view := make([]byte, 8)
	binary.LittleEndian.PutUint64(view, f.seq)
	require.NoError(t, writeFrame(peer.conn, classResume, randomNonZero(), 1, view))
}
