// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	raftapi "github.com/wippyai/runtime/api/raft"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// memberStub satisfies bootstrapMembership for watcher tests. Thread-safe.
type memberStub struct {
	mu    sync.Mutex
	local cluster.NodeInfo
	peers []cluster.NodeInfo
	meta  map[string]string
}

func newMemberStub(localID string) *memberStub {
	m := &memberStub{
		meta: make(map[string]string),
	}
	m.local = cluster.NodeInfo{ID: localID, Meta: cluster.NodeMeta{}}
	return m
}

func (m *memberStub) Nodes() []cluster.NodeInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]cluster.NodeInfo, len(m.peers))
	copy(out, m.peers)
	return out
}

func (m *memberStub) LocalNode() cluster.NodeInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := cluster.NodeInfo{ID: m.local.ID, Meta: cluster.NodeMeta{}}
	for k, v := range m.local.Meta {
		out.Meta[k] = v
	}
	return out
}

func (m *memberStub) UpdateMeta(updates map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range updates {
		m.local.Meta[k] = v
		m.meta[k] = v
	}
}

func (m *memberStub) status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.meta[raftStatusMeta]
}

func (m *memberStub) setPeers(peers []cluster.NodeInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers = append(m.peers[:0], peers...)
}

// peerWithExpect builds a peer NodeInfo with raft_status=pre advertising
// the given Expect. eligibleOverride lets a test set raft_eligible=false.
func peerWithExpect(id string, expect int, eligibleOverride string) cluster.NodeInfo {
	meta := cluster.NodeMeta{
		raftStatusMeta:      raftStatusPre,
		bootstrapExpectMeta: strconv.Itoa(expect),
	}
	if eligibleOverride != "" {
		meta["raft_eligible"] = eligibleOverride
	}
	return cluster.NodeInfo{ID: id, Meta: meta}
}

// nodeStub satisfies bootstrapNode. Thread-safe.
type nodeStub struct {
	mu              sync.Mutex
	state           raftapi.State
	leader          raftapi.ServerID
	bootstrapped    [][]string
	bootstrapErr    error
	bootstrapHook   func() error // per-call override; runs before bootstrapErr
	bootstrapCalled chan struct{}
}

func newNodeStub() *nodeStub {
	return &nodeStub{
		state:           raftapi.Follower,
		bootstrapCalled: make(chan struct{}, 4),
	}
}

func (n *nodeStub) Bootstrap(voterIDs []string) error {
	n.mu.Lock()
	hook := n.bootstrapHook
	errStatic := n.bootstrapErr
	n.mu.Unlock()

	var err error
	switch {
	case hook != nil:
		err = hook()
	case errStatic != nil:
		err = errStatic
	}

	n.mu.Lock()
	out := make([]string, len(voterIDs))
	copy(out, voterIDs)
	n.bootstrapped = append(n.bootstrapped, out)
	n.mu.Unlock()

	select {
	case n.bootstrapCalled <- struct{}{}:
	default:
	}
	return err
}

func (n *nodeStub) State() raftapi.State {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.state
}

func (n *nodeStub) Leader() (raftapi.ServerID, raftapi.ServerAddress, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.leader, raftapi.ServerAddress(n.leader), nil
}

func (n *nodeStub) setEstablished(leader string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.state = raftapi.Follower
	n.leader = raftapi.ServerID(leader)
}

func (n *nodeStub) bootstrapCalls() [][]string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([][]string, len(n.bootstrapped))
	for i, v := range n.bootstrapped {
		out[i] = append([]string(nil), v...)
	}
	return out
}

// newTestBus returns a real event bus, the watcher uses it for subscription.
func newTestBus(t *testing.T) event.Bus {
	t.Helper()
	bus := eventbus.NewBus()
	t.Cleanup(func() { bus.Stop() })
	return bus
}

// TestBootstrapWatcher_SingleNode verifies that Expect=1 bootstraps
// synchronously inside Start with just the local node as voter.
func TestBootstrapWatcher_SingleNode(t *testing.T) {
	mem := newMemberStub("node-a")
	node := newNodeStub()
	bus := newTestBus(t)

	w := NewBootstrapWatcher("node-a",
		BootstrapWatcherConfig{Expect: 1},
		node, mem, bus, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, w.Start(ctx))

	calls := node.bootstrapCalls()
	require.Len(t, calls, 1, "single-node mode bootstraps once")
	assert.Equal(t, []string{"node-a"}, calls[0])
	assert.Equal(t, raftStatusIn, mem.status(),
		"single-node bootstrap transitions to 'in'")
}

// TestBootstrapWatcher_QuorumReached verifies the multi-node path: with
// Expect=3 and 2 peers visible advertising the same expect, the watcher
// fires Bootstrap with the sorted [a,b,c] list after the grace window.
func TestBootstrapWatcher_QuorumReached(t *testing.T) {
	mem := newMemberStub("node-c")
	mem.setPeers([]cluster.NodeInfo{
		peerWithExpect("node-a", 3, ""),
		peerWithExpect("node-b", 3, ""),
	})
	node := newNodeStub()
	bus := newTestBus(t)

	w := NewBootstrapWatcher("node-c",
		BootstrapWatcherConfig{
			Expect: 3,
			Grace:  50 * time.Millisecond,
			Poll:   20 * time.Millisecond,
		},
		node, mem, bus, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.Start(ctx))

	select {
	case <-node.bootstrapCalled:
	case <-time.After(time.Second):
		t.Fatal("bootstrap not called after quorum reached")
	}

	calls := node.bootstrapCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, []string{"node-a", "node-b", "node-c"}, calls[0],
		"all nodes deterministically bootstrap with the sorted list")
	w.Stop()
	assert.Equal(t, raftStatusIn, mem.status())
}

// TestBootstrapWatcher_DefersToExistingCluster verifies that when a peer
// in the gossip view already advertises raft_status=in, the watcher does
// NOT bootstrap (it would otherwise create a split cluster). Instead it
// waits until the local raft is added by the existing leader, then
// transitions to "in".
func TestBootstrapWatcher_DefersToExistingCluster(t *testing.T) {
	mem := newMemberStub("node-late")
	// One peer in "pre" (also a late joiner), one already "in" — the
	// "in" peer is the discriminator that tells us a cluster exists.
	mem.setPeers([]cluster.NodeInfo{
		peerWithExpect("node-a", 3, ""),
		{
			ID: "node-b",
			Meta: cluster.NodeMeta{
				raftStatusMeta:      raftStatusIn,
				bootstrapExpectMeta: "3",
			},
		},
	})
	node := newNodeStub()
	bus := newTestBus(t)

	w := NewBootstrapWatcher("node-late",
		BootstrapWatcherConfig{
			Expect: 3,
			Grace:  20 * time.Millisecond,
			Poll:   10 * time.Millisecond,
		},
		node, mem, bus, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.Start(ctx))

	// Give the watcher more than a grace window to confirm it does NOT
	// bootstrap.
	time.Sleep(150 * time.Millisecond)
	assert.Empty(t, node.bootstrapCalls(),
		"watcher must not bootstrap when a peer is already 'in'")

	// Simulate the leader's reconciler having added us — local raft now
	// has a leader. Watcher should transition to 'in' and exit.
	node.setEstablished("node-b")

	require.Eventually(t, func() bool {
		return mem.status() == raftStatusIn
	}, time.Second, 10*time.Millisecond,
		"watcher transitions to 'in' once raft has a leader")
	w.Stop()
}

// TestBootstrapWatcher_BelowExpect verifies the watcher waits when fewer
// than Expect peers are visible and does not bootstrap.
func TestBootstrapWatcher_BelowExpect(t *testing.T) {
	mem := newMemberStub("node-c")
	mem.setPeers([]cluster.NodeInfo{
		peerWithExpect("node-a", 3, ""),
		// only 1 peer + self = 2, expect 3
	})
	node := newNodeStub()
	bus := newTestBus(t)

	w := NewBootstrapWatcher("node-c",
		BootstrapWatcherConfig{
			Expect: 3,
			Grace:  20 * time.Millisecond,
			Poll:   10 * time.Millisecond,
		},
		node, mem, bus, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.Start(ctx))

	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, node.bootstrapCalls(),
		"watcher must not bootstrap below Expect")
	w.Stop()
}

// TestBootstrapWatcher_IgnoresIneligiblePeer verifies that peers
// advertising raft_eligible=false are excluded from the quorum count.
func TestBootstrapWatcher_IgnoresIneligiblePeer(t *testing.T) {
	mem := newMemberStub("node-c")
	mem.setPeers([]cluster.NodeInfo{
		peerWithExpect("node-a", 3, ""),
		peerWithExpect("node-b", 3, ""),
		peerWithExpect("node-d", 3, "false"), // ineligible — does not count
	})
	node := newNodeStub()
	bus := newTestBus(t)

	w := NewBootstrapWatcher("node-c",
		BootstrapWatcherConfig{
			Expect: 3,
			Grace:  20 * time.Millisecond,
			Poll:   10 * time.Millisecond,
		},
		node, mem, bus, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.Start(ctx))

	select {
	case <-node.bootstrapCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("bootstrap not called when 3 eligible peers present")
	}
	calls := node.bootstrapCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, []string{"node-a", "node-b", "node-c"}, calls[0],
		"ineligible peer is excluded from the voter list")
	w.Stop()
}

// TestBootstrapWatcher_MismatchedExpect verifies that a peer advertising
// a different BootstrapExpect is excluded from the quorum (this prevents
// accidental cluster formation when configurations disagree).
func TestBootstrapWatcher_MismatchedExpect(t *testing.T) {
	mem := newMemberStub("node-c")
	mem.setPeers([]cluster.NodeInfo{
		peerWithExpect("node-a", 3, ""),
		peerWithExpect("node-b", 5, ""), // expects 5, we expect 3 — excluded
	})
	node := newNodeStub()
	bus := newTestBus(t)

	w := NewBootstrapWatcher("node-c",
		BootstrapWatcherConfig{
			Expect: 3,
			Grace:  20 * time.Millisecond,
			Poll:   10 * time.Millisecond,
		},
		node, mem, bus, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.Start(ctx))

	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, node.bootstrapCalls(),
		"watcher must exclude peers with mismatched BootstrapExpect")
	w.Stop()
}

// TestBootstrapWatcher_ZeroExpect verifies that Expect=0 never
// self-bootstraps, even when peers are present. The node waits for the
// existing leader's reconciler to AddVoter it.
func TestBootstrapWatcher_ZeroExpect(t *testing.T) {
	mem := newMemberStub("node-late")
	mem.setPeers([]cluster.NodeInfo{
		peerWithExpect("node-a", 3, ""),
		peerWithExpect("node-b", 3, ""),
		peerWithExpect("node-c", 3, ""),
	})
	node := newNodeStub()
	bus := newTestBus(t)

	w := NewBootstrapWatcher("node-late",
		BootstrapWatcherConfig{
			Expect: 0,
			Grace:  20 * time.Millisecond,
			Poll:   10 * time.Millisecond,
		},
		node, mem, bus, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.Start(ctx))

	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, node.bootstrapCalls(),
		"Expect=0 never self-bootstraps")

	// Simulate AddVoter happening externally; watcher should still
	// transition to "in" once raft has a leader.
	node.setEstablished("node-a")
	require.Eventually(t, func() bool {
		return mem.status() == raftStatusIn
	}, time.Second, 10*time.Millisecond)
	w.Stop()
}

// TestBootstrapWatcher_StopAfterFailedStart verifies that Stop() does not
// deadlock when Start() fails. The watcher's doneCh must be closed on
// every Start error path, otherwise Stop()'s wait blocks forever.
func TestBootstrapWatcher_StopAfterFailedStart(t *testing.T) {
	mem := newMemberStub("node-solo")
	node := newNodeStub()
	node.bootstrapErr = errors.New("simulated bootstrap failure")
	bus := newTestBus(t)

	w := NewBootstrapWatcher("node-solo",
		BootstrapWatcherConfig{Expect: 1},
		node, mem, bus, zap.NewNop())

	err := w.Start(context.Background())
	require.Error(t, err, "Expect=1 path returns the bootstrap error")

	// Stop must return promptly even though Start failed. Run it under
	// a short deadline to detect a deadlock as a fatal timeout.
	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop() deadlocked after failed Start()")
	}
}

// TestBootstrapWatcher_RetryAfterBootstrapError verifies that a bootstrap
// failure resets the stability window so the watcher retries on the next
// gossip event instead of tight-looping or staying stuck.
func TestBootstrapWatcher_RetryAfterBootstrapError(t *testing.T) {
	mem := newMemberStub("node-c")
	mem.setPeers([]cluster.NodeInfo{
		peerWithExpect("node-a", 3, ""),
		peerWithExpect("node-b", 3, ""),
	})
	node := newNodeStub()
	// First Bootstrap call fails, subsequent calls succeed.
	var attempts atomic.Int32
	node.bootstrapHook = func() error {
		if attempts.Add(1) == 1 {
			return errors.New("simulated bootstrap failure")
		}
		return nil
	}
	bus := newTestBus(t)

	w := NewBootstrapWatcher("node-c",
		BootstrapWatcherConfig{
			Expect: 3,
			Grace:  20 * time.Millisecond,
			Poll:   10 * time.Millisecond,
		},
		node, mem, bus, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, w.Start(ctx))

	// Watcher should call Bootstrap twice: once that fails, then a retry
	// after the stability window is re-established. bootstrapCalled is
	// signaled on every call.
	for i := 0; i < 2; i++ {
		select {
		case <-node.bootstrapCalled:
		case <-time.After(2 * time.Second):
			t.Fatalf("bootstrap call %d not observed", i+1)
		}
	}

	calls := node.bootstrapCalls()
	require.GreaterOrEqual(t, len(calls), 2,
		"watcher must retry bootstrap after a failure")
	w.Stop()
	assert.Equal(t, raftStatusIn, mem.status(),
		"watcher transitions to 'in' after the successful retry")
}

// TestBootstrapWatcher_GraceWindowRequired verifies the watcher does not
// fire bootstrap on the first observation of N peers — it requires the
// set to remain stable for the grace window.
func TestBootstrapWatcher_GraceWindowRequired(t *testing.T) {
	mem := newMemberStub("node-c")
	mem.setPeers([]cluster.NodeInfo{
		peerWithExpect("node-a", 3, ""),
		peerWithExpect("node-b", 3, ""),
	})
	node := newNodeStub()
	bus := newTestBus(t)

	w := NewBootstrapWatcher("node-c",
		BootstrapWatcherConfig{
			Expect: 3,
			Grace:  200 * time.Millisecond,
			Poll:   20 * time.Millisecond,
		},
		node, mem, bus, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.Start(ctx))

	// Within ~50ms (well below grace), bootstrap must not have fired.
	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, node.bootstrapCalls(),
		"watcher must wait for the grace window before firing bootstrap")

	// After the full grace window it does fire.
	select {
	case <-node.bootstrapCalled:
	case <-time.After(time.Second):
		t.Fatal("bootstrap not called after grace window elapsed")
	}
	w.Stop()
}
