// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/topology/namereg/kvbacked"
)

// TestE2E_KVRegistry_StrongPromotesOnSurvivorsAfterLeaderKill is the failure-
// reconcile capstone for Strong scope: a reservation is opened requiring every
// member plus a phantom node that never acks (so it stays pending), then the
// raft LEADER is killed. A new leader takes over and, because the phantom (and
// the dead leader) have left the membership, prunes them from RequiredNodes and
// promotes on the surviving acks — instead of blocking until the deadline. This
// exercises the leader-takeover reconcile (seed + leaderSweep -> pruneDeparted).
func TestE2E_KVRegistry_StrongPromotesOnSurvivorsAfterLeaderKill(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node strong failover reconcile test")
	}
	c := NewCluster(t, 3)

	// Dynamic membership: every real node plus a phantom that never acks. Guarded
	// because the strong plane reads it from reconcile/sweep goroutines.
	var mu sync.Mutex
	members := []pid.NodeID{"ghost"}
	for _, n := range c.Nodes() {
		members = append(members, n.ID)
	}
	membership := func() []pid.NodeID {
		mu.Lock()
		defer mu.Unlock()
		return append([]pid.NodeID(nil), members...)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	regs := make(map[string]*kvbacked.Service, len(c.Nodes()))
	for _, n := range c.Nodes() {
		node := n
		reg := kvbacked.NewService(node.KV, node.ID, nil, nil)
		reg.ConfigureStrong(kvbacked.StrongDeps{
			Membership: membership,
			IsLeader:   func() bool { return node.Raft.IsLeader() },
			Deadline:   20 * time.Second, // long: must promote via prune, not expiry
		})
		if err := reg.StartReconciler(ctx); err != nil {
			t.Fatalf("start reconciler on %s: %v", node.ID, err)
		}
		regs[node.ID] = reg
	}

	leader := c.Leader()
	f := c.Follower() // registrant survives the leader kill
	p := pid.PID{Node: f.ID, Host: "proc", UniqID: "s1"}
	done := make(chan globalapi.RegisterOutcome, 1)
	errc := make(chan error, 1)
	go func() {
		out, err := regs[f.ID].RegisterScope(context.Background(), "strongsvc", p, globalapi.Strong)
		if err != nil {
			errc <- err
			return
		}
		done <- out
	}()

	// Reservation must reach the pending window (real nodes ack; phantom never does).
	if !waitReserved(t, regs[f.ID], "strongsvc", 8*time.Second) {
		t.Fatalf("strong reservation never became pending")
	}

	// Kill the leader. A survivor wins election and inherits the pending.
	for i, n := range c.Nodes() {
		if n == leader {
			c.Kill(i)
			break
		}
	}
	newLeader := c.WaitLeader(10 * time.Second)
	if newLeader == leader {
		t.Fatalf("leader did not change after kill")
	}

	// Gossip drops the phantom and the dead leader from the live membership; the
	// new leader must prune both from RequiredNodes and promote on the survivors.
	mu.Lock()
	var survivors []pid.NodeID
	for _, n := range c.Nodes() {
		if n != leader {
			survivors = append(survivors, n.ID)
		}
	}
	members = survivors
	mu.Unlock()

	select {
	case out := <-done:
		if out.State != globalapi.RegisterStateActive || out.PID.String() != p.String() {
			t.Fatalf("want Active on survivors after leader kill, got %+v", out)
		}
	case err := <-errc:
		t.Fatalf("reservation did not promote on survivors after leader kill: %v", err)
	case <-time.After(18 * time.Second):
		t.Fatalf("reservation neither promoted nor failed after leader kill + membership drop")
	}

	for _, n := range c.Nodes() {
		if n == leader {
			continue
		}
		waitLookup(t, regs[n.ID], "strongsvc", p, 8*time.Second)
	}
}

// TestE2E_KVRegistry_ConsistentReapedOnNodeDrop proves the node-failure reap path
// for CONSISTENT names: a name owned by a node that leaves is removed cluster-wide
// via DropNode -> RemoveNode -> reap. The harness delivers the departure manually
// (the boot NodeLeft subscription that calls DropNode is not wired into the
// test-constructed Service), mirroring locks_e2e's explicit ReapNode.
func TestE2E_KVRegistry_ConsistentReapedOnNodeDrop(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node consistent reap test")
	}
	c := NewCluster(t, 3)
	regOf := func(n *Node) *kvbacked.Service { return kvbacked.NewService(n.KV, n.ID, nil, nil) }

	owner := c.Follower()
	p := pid.PID{Node: owner.ID, Host: "proc", UniqID: "r1"}
	if _, err := regOf(owner).RegisterScope(context.Background(), "svc", p, globalapi.Consistent); err != nil {
		t.Fatalf("register: %v", err)
	}
	for _, n := range c.Nodes() {
		waitLookup(t, regOf(n), "svc", p, 5*time.Second)
	}

	// The owner node leaves; a survivor drives the reap (as NodeLeft -> DropNode would).
	var survivor *Node
	for i, n := range c.Nodes() {
		if n == owner {
			c.Kill(i)
		} else if survivor == nil {
			survivor = n
		}
	}
	c.WaitLeader(10 * time.Second)
	regOf(survivor).DropNode(owner.ID)

	for _, n := range c.Nodes() {
		if n == owner {
			continue
		}
		waitNoLookup(t, regOf(n), "svc", 8*time.Second)
	}
}

func waitReserved(t *testing.T, r *kvbacked.Service, name string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, ok := r.IsStrongReserved(name); ok {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, ok := r.IsStrongReserved(name)
	return ok
}
