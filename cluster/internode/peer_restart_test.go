// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

// restartNode is a manager recording deliveries and session ends.
type restartNode struct {
	manager  *manager
	received [][]byte
	ended    []cluster.NodeID
	mu       sync.Mutex
}

func startRestartNode(t *testing.T, self, peer cluster.NodeID) *restartNode {
	t.Helper()
	cfg := insecureManagerConfig()
	cfg.LocalNodeID = self
	cfg.BindAddr = "127.0.0.1"
	cfg.BindPort = 0
	cfg.Logger = zap.NewNop()
	cfg.InitialRetryDelay = time.Millisecond
	cfg.MaxRetryDelay = 10 * time.Millisecond
	n := &restartNode{manager: NewConnectionManager(cfg, nil).(*manager)}
	require.NoError(t, n.manager.Start(context.Background(),
		func(_ cluster.NodeID, data []byte) {
			n.mu.Lock()
			n.received = append(n.received, append([]byte(nil), data...))
			n.mu.Unlock()
		},
		func(id cluster.NodeID) {
			n.mu.Lock()
			n.ended = append(n.ended, id)
			n.mu.Unlock()
		}))
	n.manager.AddManagedNode(peer)
	return n
}

func (n *restartNode) snapshot() (received []string, ended []cluster.NodeID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, r := range n.received {
		received = append(received, string(r))
	}
	return received, append([]cluster.NodeID(nil), n.ended...)
}

// A peer restarting under the same node ID with a new incarnation ends the
// session: frames queued for the old process are discarded and signalled,
// and the new process receives only frames sent after the restart.
func TestPeerRestartEndsSessionAndDiscardsItsFrames(t *testing.T) {
	a := startRestartNode(t, "node-a", "node-b")
	t.Cleanup(func() { require.NoError(t, a.manager.Stop()) })
	b1 := startRestartNode(t, "node-b", "node-a")
	a.manager.EnsureConnection("node-b", "127.0.0.1", b1.manager.GetListenPort())
	require.Eventually(t, func() bool {
		_, s := a.manager.nodeStates.GetNodeConnection("node-b")
		return s == StateConnected
	}, 5*time.Second, time.Millisecond)
	require.NoError(t, a.manager.SendToNode("node-b", []byte("before"), ClassRaftControl))
	require.Eventually(t, func() bool { got, _ := b1.snapshot(); return len(got) == 1 }, 5*time.Second, time.Millisecond)

	// The old process dies; frames for it queue on A.
	require.NoError(t, b1.manager.Stop())
	for range 50 {
		require.NoError(t, a.manager.SendToNode("node-b", []byte("for-old-process"), ClassPGBroadcast))
	}
	_, ended := a.snapshot()
	require.Empty(t, ended)

	// The node restarts under the same ID with a new incarnation.
	b2 := startRestartNode(t, "node-b", "node-a")
	t.Cleanup(func() { require.NoError(t, b2.manager.Stop()) })
	require.NotEqual(t, b1.manager.Incarnation(), b2.manager.Incarnation())
	a.manager.EnsureConnection("node-b", "127.0.0.1", b2.manager.GetListenPort())

	require.Eventually(t, func() bool {
		_, ended := a.snapshot()
		return len(ended) == 1
	}, 5*time.Second, time.Millisecond)
	require.NoError(t, a.manager.SendToNode("node-b", []byte("fresh"), ClassPGBroadcast))
	require.NoError(t, b2.manager.SendToNode("node-a", []byte("from-new-process"), ClassPGBroadcast))
	require.Eventually(t, func() bool { got, _ := b2.snapshot(); return len(got) > 0 }, 5*time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		got, _ := a.snapshot()
		return len(got) == 1
	}, 5*time.Second, time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	got, _ := b2.snapshot()
	require.Equal(t, []string{"fresh"}, got, "the new process received frames of the old session")
	fromB, ended := a.snapshot()
	require.Equal(t, []string{"from-new-process"}, fromB)
	require.Equal(t, []cluster.NodeID{"node-b"}, ended)

	// The handshake recognized the restart: the session carries the new
	// incarnation and the old one is retired.
	state := a.manager.nodeStates.GetNodeState("node-b")
	state.queueMu.Lock()
	defer state.queueMu.Unlock()
	require.Equal(t, b2.manager.Incarnation(), state.session.peerIncarnation)
	require.Equal(t, []uint64{b1.manager.Incarnation()}, state.retired)
}

// A superseded incarnation cannot rebind the session.
func TestRetiredIncarnationIsRejected(t *testing.T) {
	nsm := setupStateManager()
	nsm.CreateNodeState("peer")
	state := nsm.GetNodeState("peer")
	require.Equal(t, incarnationBound, nsm.bindPeerIncarnation("peer", state, 1))
	require.Equal(t, incarnationBound, nsm.bindPeerIncarnation("peer", state, 1))
	require.Equal(t, incarnationRestarted, nsm.bindPeerIncarnation("peer", state, 2))
	require.Equal(t, incarnationRejected, nsm.bindPeerIncarnation("peer", state, 1))
	require.Equal(t, incarnationBound, nsm.bindPeerIncarnation("peer", state, 2))
}

// A delayed departure of a node's previous incarnation keeps the session its
// successor bound; the departure of the bound incarnation ends it.
func TestRemoveKeepsSessionOfSuccessorIncarnation(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.Logger = zap.NewNop()
	m := NewConnectionManager(cfg, nil).(*manager)
	var ended []cluster.NodeID
	m.nodeStates.sessionEnded = func(id cluster.NodeID) { ended = append(ended, id) }
	m.AddManagedNode("peer")
	state := m.nodeStates.GetNodeState("peer")
	require.Equal(t, incarnationBound, m.nodeStates.bindPeerIncarnation("peer", state, 7))
	require.NoError(t, m.SendToNode("peer", []byte("for-successor"), ClassRaftControl))

	m.RemoveManagedNode("peer", 6)
	require.Same(t, state, m.nodeStates.GetNodeState("peer"))
	require.Empty(t, ended)
	require.Equal(t, [][]byte{[]byte("for-successor")}, drainAllData(m.nodeStates, "peer"))

	m.RemoveManagedNode("peer", 7)
	require.Nil(t, m.nodeStates.GetNodeState("peer"))
	require.Equal(t, []cluster.NodeID{"peer"}, ended)
}
