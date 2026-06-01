// SPDX-License-Identifier: MPL-2.0

//go:build integration
// +build integration

package raft

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	hraft "github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap/zaptest"
)

// meshFabric ties N in-process internode.ConnectionManagers together
// so SendToNode on any one dispatches synchronously into the target's
// registered class receiver. There is no real TCP; frames arrive
// in-order, in-memory. Used by the integration tests to exercise raft
// over the mesh transport without spinning up real network endpoints.
type meshFabric struct {
	mu    sync.Mutex
	conns map[cluster.NodeID]*meshConn
}

func newMeshFabric() *meshFabric {
	return &meshFabric{conns: map[cluster.NodeID]*meshConn{}}
}

func (f *meshFabric) connect(id cluster.NodeID) *meshConn {
	c := &meshConn{
		fabric:    f,
		self:      id,
		receivers: map[internode.Class]func(cluster.NodeID, []byte){},
		overflow:  map[internode.Class]func(cluster.NodeID){},
	}
	f.mu.Lock()
	f.conns[id] = c
	f.mu.Unlock()
	return c
}

// meshConn implements internode.ConnectionManager for a single endpoint
// in a meshFabric. Outbound frames are looked up by target NodeID in the
// fabric and handed to the target's registered receiver.
type meshConn struct {
	fabric    *meshFabric
	self      cluster.NodeID
	mu        sync.Mutex
	receivers map[internode.Class]func(cluster.NodeID, []byte)
	overflow  map[internode.Class]func(cluster.NodeID)
}

func (c *meshConn) Start(_ context.Context, _ func(cluster.NodeID, []byte)) error {
	return nil
}
func (c *meshConn) Stop() error { return nil }

func (c *meshConn) SendToNode(target cluster.NodeID, data []byte, class internode.Class) error {
	c.fabric.mu.Lock()
	peer := c.fabric.conns[target]
	c.fabric.mu.Unlock()
	if peer == nil {
		return fmt.Errorf("mesh fabric: unknown peer %q", target)
	}
	peer.mu.Lock()
	r := peer.receivers[class]
	peer.mu.Unlock()
	if r == nil {
		return errors.New("mesh fabric: no receiver registered for class")
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	r(c.self, cp)
	return nil
}

func (c *meshConn) EnsureConnection(_ cluster.NodeID, _ string, _ int) {}
func (c *meshConn) DisconnectFromNode(_ cluster.NodeID)                {}
func (c *meshConn) ConnectedNodes() []cluster.NodeID                   { return nil }
func (c *meshConn) GetListenPort() int                                 { return 0 }
func (c *meshConn) AddManagedNode(_ cluster.NodeID)                    {}
func (c *meshConn) RemoveManagedNode(_ cluster.NodeID)                 {}
func (c *meshConn) IsManaged(_ cluster.NodeID) bool                    { return true }
func (c *meshConn) EvictOrphanNodes(_ map[cluster.NodeID]struct{}) int { return 0 }
func (c *meshConn) RecordDropReason(_ string)                          {}

func (c *meshConn) RegisterClassReceiver(class internode.Class, recv func(cluster.NodeID, []byte)) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if recv != nil && c.receivers[class] != nil {
		return false
	}
	c.receivers[class] = recv
	return true
}

func (c *meshConn) RegisterClassOverflowHandler(class internode.Class, handler func(cluster.NodeID)) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if handler != nil && c.overflow[class] != nil {
		return false
	}
	c.overflow[class] = handler
	return true
}

var _ internode.ConnectionManager = (*meshConn)(nil)

// noopFSM is a state machine that ignores all input. Sufficient for
// membership-focused integration tests where we don't exercise log replication
// semantics.
type noopFSM struct{}

func (noopFSM) Apply(_ *hraft.Log) any               { return nil }
func (noopFSM) Snapshot() (hraft.FSMSnapshot, error) { return noopSnap{}, nil }
func (noopFSM) Restore(rc io.ReadCloser) error       { return rc.Close() }

type noopSnap struct{}

func (noopSnap) Persist(s hraft.SnapshotSink) error { return s.Close() }
func (noopSnap) Release()                           {}

// staticMembership is a cluster.Membership that returns a fixed snapshot.
// Tests mutate the slice under the lock between reconcile passes.
type staticMembership struct {
	mu    sync.Mutex
	local cluster.NodeInfo
	all   []cluster.NodeInfo
}

func (m *staticMembership) Nodes() []cluster.NodeInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]cluster.NodeInfo, len(m.all))
	copy(out, m.all)
	return out
}

func (m *staticMembership) LocalNode() cluster.NodeInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.local
}

func (m *staticMembership) UpdateMeta(map[string]string) {}

func (m *staticMembership) replace(local cluster.NodeInfo, all []cluster.NodeInfo) {
	m.mu.Lock()
	m.local = local
	m.all = all
	m.mu.Unlock()
}

// startNode brings up a single in-memory Raft node attached to fabric for
// testing. bootstrap should be true exactly on one node per cluster (the
// seed) — that node calls Bootstrap([self]) after Start, which the
// membership reconciler then grows into the full cluster via AddVoter.
// This sidesteps the gossip-driven BootstrapWatcher (which would need a
// real membership implementation); these integration tests drive raft
// directly via the reconciler.
func startNode(t *testing.T, fabric *meshFabric, id string, bootstrap bool) *Node {
	t.Helper()
	cfg := raftapi.Config{
		// Defaults are fine for in-process loopback; HeartbeatTimeout/ElectionTimeout
		// must remain >= LeaderLeaseTimeout (500ms from hashicorp DefaultConfig).
		HeartbeatTimeout: 600 * time.Millisecond,
		ElectionTimeout:  600 * time.Millisecond,
		CommitTimeout:    50 * time.Millisecond,
	}
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)

	n := NewNode(id, noopFSM{}, cfg, bus, zaptest.NewLogger(t).Named(id), nil, nil, nil)
	n.SetConnectionManager(fabric.connect(id))
	statusCh, err := n.Start(context.Background())
	require.NoError(t, err)
	go func() {
		for range statusCh {
		}
	}()
	if bootstrap {
		require.NoError(t, n.Bootstrap([]string{id}))
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	return n
}

// waitForLeader blocks until the node becomes leader or timeout fires.
func waitForLeader(t *testing.T, n *Node, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if n.IsLeader() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("node did not become leader within %s", timeout)
}

// asMember builds a NodeInfo for the membership snapshot from a live
// node. Under the mesh transport the raft ServerAddress equals the
// NodeID, so we set Addr to the same value (matches selection.go).
func asMember(n *Node) cluster.NodeInfo {
	return cluster.NodeInfo{
		ID:   n.localID,
		Addr: n.localID,
		Meta: cluster.NodeMeta{
			"raft_eligible": "true",
		},
	}
}

// TestIntegration_VoterCapAcrossSevenNodes spins up 7 in-process Raft nodes,
// hands them all to the leader's reconciler with MaxVoters=5, and asserts
// the cluster converges to exactly 5 voters + 2 nonvoters.
//
// This is the end-to-end happy-path proof that the cap actually applies
// against a real hashicorp/raft state machine.
func TestIntegration_VoterCapAcrossSevenNodes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	fabric := newMeshFabric()
	const total = 7
	nodes := make([]*Node, total)
	for i := 0; i < total; i++ {
		nodes[i] = startNode(t, fabric, fmt.Sprintf("node-%d", i), i == 0)
	}
	waitForLeader(t, nodes[0], 5*time.Second)

	// Build membership snapshot pointing the leader at all 7 nodes.
	all := make([]cluster.NodeInfo, total)
	for i, n := range nodes {
		all[i] = asMember(n)
	}
	mem := &staticMembership{local: all[0], all: all}

	// Run reconciler on the leader.
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	h := NewMembershipHandler(nodes[0], mem, bus, HandlerConfig{
		MaxVoters:         5,
		ReconcileDebounce: 50 * time.Millisecond,
		ReconcileTimeout:  3 * time.Second,
	}, zaptest.NewLogger(t).Named("handler"))
	require.NoError(t, h.Start(context.Background()))
	t.Cleanup(h.Stop)

	// Trigger reconcile by sending a NodeJoined event.
	bus.Send(context.Background(), event.Event{System: cluster.System, Kind: cluster.NodeJoined})

	// Wait for convergence: 5 voters + 2 nonvoters.
	require.Eventually(t, func() bool {
		cfg, err := nodes[0].GetConfiguration()
		if err != nil {
			return false
		}
		voters, nonvoters := 0, 0
		for _, s := range cfg {
			if s.IsVoter {
				voters++
			} else {
				nonvoters++
			}
		}
		return voters == 5 && nonvoters == 2
	}, 10*time.Second, 100*time.Millisecond, "cluster did not converge to 5 voters + 2 nonvoters")
}

// TestIntegration_GrowFromOneToFive starts with a bootstrapped single-node
// cluster, then progressively reveals new nodes via membership snapshots.
// Asserts the voter set grows along the odd ladder: 1 → 3 → 5 (capped).
func TestIntegration_GrowFromOneToFive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	fabric := newMeshFabric()
	leader := startNode(t, fabric, "node-0", true)
	waitForLeader(t, leader, 5*time.Second)

	more := []*Node{
		startNode(t, fabric, "node-1", false),
		startNode(t, fabric, "node-2", false),
		startNode(t, fabric, "node-3", false),
		startNode(t, fabric, "node-4", false),
	}

	mem := &staticMembership{local: asMember(leader), all: []cluster.NodeInfo{asMember(leader)}}
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	h := NewMembershipHandler(leader, mem, bus, HandlerConfig{
		MaxVoters:         5,
		ReconcileDebounce: 30 * time.Millisecond,
		ReconcileTimeout:  3 * time.Second,
	}, zaptest.NewLogger(t).Named("handler"))
	require.NoError(t, h.Start(context.Background()))
	t.Cleanup(h.Stop)

	// Reveal nodes one at a time and check the ladder.
	expectVoters := []int{1, 1, 3, 3, 5} // pool sizes 1..5
	all := []cluster.NodeInfo{asMember(leader)}
	for i, want := range expectVoters {
		if i > 0 {
			all = append(all, asMember(more[i-1]))
			mem.replace(all[0], all)
			bus.Send(context.Background(), event.Event{System: cluster.System, Kind: cluster.NodeJoined})
		}
		require.Eventually(t, func() bool {
			cfg, err := leader.GetConfiguration()
			if err != nil {
				return false
			}
			voters := 0
			for _, s := range cfg {
				if s.IsVoter {
					voters++
				}
			}
			return voters == want
		}, 5*time.Second, 50*time.Millisecond,
			"step %d: pool=%d expected %d voters", i, len(all), want)
	}
}

// TestIntegration_DemoteOnShrink starts with 5 voters, drops 2 from the
// membership snapshot, and asserts the cluster shrinks to 3 voters.
func TestIntegration_DemoteOnShrink(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	fabric := newMeshFabric()
	leader := startNode(t, fabric, "node-0", true)
	waitForLeader(t, leader, 5*time.Second)
	others := []*Node{
		startNode(t, fabric, "node-1", false),
		startNode(t, fabric, "node-2", false),
		startNode(t, fabric, "node-3", false),
		startNode(t, fabric, "node-4", false),
	}
	all := []cluster.NodeInfo{asMember(leader)}
	for _, n := range others {
		all = append(all, asMember(n))
	}

	mem := &staticMembership{local: all[0], all: all}
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	h := NewMembershipHandler(leader, mem, bus, HandlerConfig{
		MaxVoters:         5,
		ReconcileDebounce: 30 * time.Millisecond,
		ReconcileTimeout:  3 * time.Second,
	}, zaptest.NewLogger(t).Named("handler"))
	require.NoError(t, h.Start(context.Background()))
	t.Cleanup(h.Stop)

	bus.Send(context.Background(), event.Event{System: cluster.System, Kind: cluster.NodeJoined})

	// Wait for 5 voters.
	require.Eventually(t, func() bool {
		cfg, _ := leader.GetConfiguration()
		v := 0
		for _, s := range cfg {
			if s.IsVoter {
				v++
			}
		}
		return v == 5
	}, 5*time.Second, 50*time.Millisecond)

	// Now drop 2 nodes from the membership snapshot.
	mem.replace(all[0], all[:3])
	bus.Send(context.Background(), event.Event{System: cluster.System, Kind: cluster.NodeLeft})

	require.Eventually(t, func() bool {
		cfg, _ := leader.GetConfiguration()
		v, total := 0, 0
		for _, s := range cfg {
			total++
			if s.IsVoter {
				v++
			}
		}
		return v == 3 && total == 3
	}, 5*time.Second, 50*time.Millisecond, "cluster did not shrink to 3 voters")

	// Sanity: leader is still leading.
	assert.True(t, leader.IsLeader())
}
