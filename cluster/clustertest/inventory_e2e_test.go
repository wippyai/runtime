// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"reflect"
	"testing"
	"time"

	"github.com/wippyai/runtime/system/topology/namereg/inventory"
)

// TestE2E_InventoryFollowerFailoverAndRestart proves the inert inventory uses
// the real follower-forwarding path and survives a leader election plus a
// durable restart. The inventory transition is only a record mutation; this
// test intentionally makes no admission/readiness claim for it.
func TestE2E_InventoryFollowerFailoverAndRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node inventory failover test")
	}
	c := NewCluster(t, 3)
	follower := c.Follower()
	store, err := inventory.New(follower.KV, inventory.Options{})
	if err != nil {
		t.Fatalf("new follower inventory: %v", err)
	}
	snap, rev, err := store.Initialize("inventory-e2e")
	if err != nil {
		t.Fatalf("initialize from follower: %v", err)
	}
	// A forwarded write may be committed before the follower has applied its
	// local FSM entry; the next transition must use an observed local revision.
	waitInventory(t, c.Nodes(), snap)
	snap, rev, err = store.Register(rev, inventory.Participant{
		Node: follower.ID,
		Incarnation: inventory.Incarnation{
			Sequence:  1,
			BootNonce: []byte("boot-1"),
		},
	})
	if err != nil {
		t.Fatalf("register from follower: %v", err)
	}
	if snap.Generation != 2 || len(snap.Slots) != 1 {
		t.Fatalf("unexpected follower transition: %+v", snap)
	}
	waitInventory(t, c.Nodes(), snap)

	leader := c.Leader()
	leaderIndex := -1
	for i, n := range c.Nodes() {
		if n == leader {
			leaderIndex = i
			break
		}
	}
	if leaderIndex < 0 {
		t.Fatal("leader is not in cluster node list")
	}
	c.Kill(leaderIndex)
	newLeader := c.WaitLeader(10 * time.Second)
	if newLeader == leader {
		t.Fatal("leader did not change after kill")
	}
	waitInventory(t, c.Nodes()[0:leaderIndex], snap)
	waitInventory(t, c.Nodes()[leaderIndex+1:], snap)
	newStore, err := inventory.New(newLeader.KV, inventory.Options{})
	if err != nil {
		t.Fatalf("new inventory after failover: %v", err)
	}
	postFailoverNode := ""
	for _, n := range c.Nodes() {
		if n.ID != follower.ID && n.ID != leader.ID {
			postFailoverNode = n.ID
			break
		}
	}
	if postFailoverNode == "" {
		t.Fatal("no other live node to enroll after failover")
	}
	snap, _, err = newStore.Register(rev, inventory.Participant{
		Node: postFailoverNode,
		Incarnation: inventory.Incarnation{
			Sequence: 1, BootNonce: []byte("after-failover"),
		},
	})
	if err != nil {
		t.Fatalf("register after failover: %v", err)
	}
	waitInventory(t, c.Nodes()[0:leaderIndex], snap)
	waitInventory(t, c.Nodes()[leaderIndex+1:], snap)

	// Rebuild the killed node from its durable raft directory and check that
	// the retained record is restored after it catches up.
	c.Restart(leaderIndex)
	waitInventory(t, c.Nodes(), snap)
	if _, err := c.Node(leaderIndex).KV.Get(inventory.Key); err != nil {
		t.Fatalf("restarted node lost inventory key: %v", err)
	}
}

func waitInventory(t *testing.T, nodes []*Node, want inventory.Snapshot) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		all := true
		for _, node := range nodes {
			store, err := inventory.New(node.KV, inventory.Options{})
			if err != nil {
				t.Fatalf("new inventory on %s: %v", node.ID, err)
			}
			got, _, err := store.ReadObserved()
			if err != nil || !reflect.DeepEqual(got, want) {
				all = false
				break
			}
		}
		if all {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("inventory did not converge to generation %d", want.Generation)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
