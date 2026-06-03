// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"errors"
	"testing"
	"time"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// TestE2E_TxnReplicatesFromFollower proves an atomic multi-key txn issued on a
// follower forwards to the leader, commits all-or-nothing, and replicates to
// every node; then a CompareAndDelete from a follower removes a key cluster-wide.
func TestE2E_TxnReplicatesFromFollower(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node txn test")
	}
	c := NewCluster(t, 3)
	f := c.Follower()

	committed, err := f.KV.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: "t:a", Value: []byte("1")},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: "t:b", Value: []byte("2")},
	})
	if err != nil || !committed {
		t.Fatalf("txn from follower: committed=%v err=%v", committed, err)
	}
	for _, n := range c.Nodes() {
		waitKV(t, n, "t:a", "1", 5*time.Second)
		waitKV(t, n, "t:b", "2", 5*time.Second)
	}

	e, err := f.KV.Get("t:a")
	if err != nil {
		t.Fatalf("get for version: %v", err)
	}
	ok, err := f.KV.CompareAndDelete("t:a", e.Version)
	if err != nil || !ok {
		t.Fatalf("CAD from follower: ok=%v err=%v", ok, err)
	}
	for _, n := range c.Nodes() {
		waitGone(t, n, "t:a", 5*time.Second)
	}
}

// TestE2E_EpochMatchesAcrossNodes proves every replica stamps the same key with
// the same Epoch (the raft log index), so the value is index-comparable for the
// dissem dot and strong-scope fence.
func TestE2E_EpochMatchesAcrossNodes(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node epoch test")
	}
	c := NewCluster(t, 3)
	f := c.Follower()

	if _, err := f.KV.Set("e:k", []byte("v")); err != nil {
		t.Fatalf("set: %v", err)
	}
	for _, n := range c.Nodes() {
		waitKV(t, n, "e:k", "v", 5*time.Second)
	}

	var epoch uint64
	for i, n := range c.Nodes() {
		e, err := n.KV.Get("e:k")
		if err != nil {
			t.Fatalf("node %s get: %v", n.ID, err)
		}
		if e.Epoch == 0 {
			t.Fatalf("node %s: epoch must be the raft index, got 0", n.ID)
		}
		if i == 0 {
			epoch = e.Epoch
			continue
		}
		if e.Epoch != epoch {
			t.Fatalf("epoch disagreement: node %s has %d, want %d", n.ID, e.Epoch, epoch)
		}
	}
}

func waitGone(t *testing.T, n *Node, key string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := n.KV.Get(key); errors.Is(err, kvapi.ErrKeyNotFound) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("node %s: key %q still present", n.ID, key)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
