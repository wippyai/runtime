// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/topology/namereg/kvbacked"
)

// TestE2E_KVRegistry_StrongFailsClosedAfterLeaderKill is the failure-reconcile
// capstone for Strong scope: a reservation is opened requiring every participant
// plus a phantom node that never acks, then the raft LEADER is killed. A new
// leader inherits the committed RequiredNodes and times out when the phantom
// acknowledgement never arrives. Node departure cannot authorize promotion.
func TestE2E_KVRegistry_StrongFailsClosedAfterLeaderKill(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node strong failover reconcile test")
	}
	c := NewCluster(t, 3)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	regs := make(map[string]*kvbacked.Service, len(c.Nodes()))
	for _, n := range c.Nodes() {
		node := n
		reg := kvbacked.NewService(node.KV, node.ID, nil, nil)
		reg.ConfigureStrong(kvbacked.StrongDeps{
			Members: func() ([]pid.NodeID, error) {
				members, err := strongObserverMembers(node)
				return append(members, "ghost"), err
			},
			IsLeader: func() bool { return node.Raft.IsLeader() },
			Deadline: 2 * time.Second,
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
	if !waitCapturedObserver(t, f, "strongsvc", "ghost", 8*time.Second) {
		t.Fatalf("Strong pending never captured its observer cohort")
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

	// The captured cohort is immutable for this attempt. The new leader must
	// retain the phantom observer even if its own configuration view changes.

	select {
	case out := <-done:
		t.Fatalf("reservation promoted without all RequiredNodes acknowledgements: %+v", out)
	case err := <-errc:
		var te *globalapi.StrongRegistrationTimeoutError
		if !errors.As(err, &te) && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("want fail-closed Strong timeout, got %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("reservation neither failed closed nor timed out after leader kill")
	}
}

// TestE2E_KVRegistry_ConsistentReapedOnNodeDrop proves the node-failure reap path
// for CONSISTENT names: a name owned by a node that leaves is removed cluster-wide
// via an explicit, authorized RemoveNode call from the test harness. NodeLeft
// discovery is not an authority to reap active names; process exit and explicit
// unregister paths remain so.
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

	// The owner node leaves; an explicit RemoveNode call drives the reap.
	var survivor *Node
	for i, n := range c.Nodes() {
		if n == owner {
			c.Kill(i)
		} else if survivor == nil {
			survivor = n
		}
	}
	c.WaitLeader(10 * time.Second)
	if err := regOf(survivor).RemoveNode(context.Background(), owner.ID); err != nil {
		t.Fatalf("remove node: %v", err)
	}

	for _, n := range c.Nodes() {
		if n == owner {
			continue
		}
		waitNoLookup(t, regOf(n), "svc", 8*time.Second)
	}
}

func waitCapturedObserver(t *testing.T, node *Node, name, observer string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if entry, err := node.KV.Get("_sys:registry:pending:" + name); err == nil {
			var header struct {
				Required []pid.NodeID `codec:"r"`
			}
			if err := codec.NewDecoderBytes(entry.Value, &codec.MsgpackHandle{}).Decode(&header); err != nil {
				t.Fatalf("decode pending cohort: %v", err)
			}
			if slices.Contains(header.Required, observer) {
				return true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
