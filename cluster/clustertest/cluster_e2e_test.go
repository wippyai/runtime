// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitKV(t *testing.T, n *Node, key, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if e, err := n.KV.Get(key); err == nil && string(e.Value) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("node %s did not see %s=%s within %s", n.ID, key, want, timeout)
}

// TestE2E_KVReplicatesFromFollower proves a write issued on a FOLLOWER is
// forwarded over the relay to the leader, committed through real raft, and
// replicated to every node.
func TestE2E_KVReplicatesFromFollower(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node raft test")
	}
	c := NewCluster(t, 3)
	f := c.Follower()
	if _, err := f.KV.Set("k", []byte("v")); err != nil {
		t.Fatalf("follower set: %v", err)
	}
	for _, n := range c.Nodes() {
		waitKV(t, n, "k", "v", 5*time.Second)
	}
}

// TestE2E_LeaderFailover proves writes survive and continue after the leader is
// hard-killed and a new leader is elected.
func TestE2E_LeaderFailover(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node raft test")
	}
	c := NewCluster(t, 3)
	leader := c.Leader()
	if _, err := leader.KV.Set("before", []byte("1")); err != nil {
		t.Fatalf("set before: %v", err)
	}
	// Kill the leader.
	for i, n := range c.Nodes() {
		if n == leader {
			c.Kill(i)
			break
		}
	}
	newLeader := c.WaitLeader(10 * time.Second)
	if _, err := newLeader.KV.Set("after", []byte("2")); err != nil {
		t.Fatalf("set after failover: %v", err)
	}
	// Surviving nodes must hold both writes.
	for _, n := range c.Nodes() {
		if n == leader {
			continue
		}
		waitKV(t, n, "before", "1", 5*time.Second)
		waitKV(t, n, "after", "2", 5*time.Second)
	}
}

// TestE2E_PartitionHeal proves a partitioned follower misses writes while
// isolated and catches up after the partition heals.
func TestE2E_PartitionHeal(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node raft test")
	}
	c := NewCluster(t, 3)
	// Partition a follower.
	var pIdx = -1
	for i, n := range c.Nodes() {
		if !n.Raft.IsLeader() {
			pIdx = i
			break
		}
	}
	c.Partition(pIdx)

	leader := c.WaitLeader(10 * time.Second)
	if _, err := leader.KV.Set("p", []byte("v")); err != nil {
		t.Fatalf("set during partition: %v", err)
	}
	// Quorum of the other two has it.
	for i, n := range c.Nodes() {
		if i == pIdx {
			continue
		}
		waitKV(t, n, "p", "v", 5*time.Second)
	}
	// Heal: the isolated node catches up.
	c.Heal(pIdx)
	waitKV(t, c.Node(pIdx), "p", "v", 30*time.Second)
}

// TestE2E_DurableRestart proves fs durability: a single-node cluster's write
// survives a hard kill + restart that can only recover from disk (raft-wal +
// bbolt), and the wal/stable files exist.
func TestE2E_DurableRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("real raft durability test")
	}
	c := NewCluster(t, 1)
	if _, err := c.Node(0).KV.Set("durable", []byte("yes")); err != nil {
		t.Fatalf("set: %v", err)
	}
	// Give the commit a moment to hit disk, then verify files exist.
	time.Sleep(200 * time.Millisecond)
	raftDir := filepath.Join(c.Node(0).dataDir, "_sys", "raft")
	if _, err := os.Stat(filepath.Join(raftDir, "wal")); err != nil {
		t.Fatalf("wal dir missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(raftDir, "stable.bolt")); err != nil {
		t.Fatalf("stable.bolt missing: %v", err)
	}

	c.Kill(0)
	c.Restart(0)
	c.WaitLeader(10 * time.Second)
	waitKV(t, c.Node(0), "durable", "yes", 5*time.Second)
}

// TestE2E_DurableRestartPreservesEpoch proves the per-key epoch (== raft log
// index, the dissem LWW dot and strong join fence) survives a kill+restart
// unchanged AND that post-restart writes continue stamping strictly greater
// epochs — i.e. the epoch counter never resets to 0 across a snapshot/restore.
func TestE2E_DurableRestartPreservesEpoch(t *testing.T) {
	if testing.Short() {
		t.Skip("real raft durability test")
	}
	c := NewCluster(t, 1)
	if _, err := c.Node(0).KV.Set("ep", []byte("v1")); err != nil {
		t.Fatalf("set: %v", err)
	}
	before, err := c.Node(0).KV.Get("ep")
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	if before.Epoch == 0 {
		t.Fatalf("epoch must be the raft index, got 0")
	}
	time.Sleep(200 * time.Millisecond)

	c.Kill(0)
	c.Restart(0)
	c.WaitLeader(10 * time.Second)
	waitKV(t, c.Node(0), "ep", "v1", 5*time.Second)

	after, err := c.Node(0).KV.Get("ep")
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if after.Epoch != before.Epoch {
		t.Fatalf("epoch must be preserved across restart: before=%d after=%d", before.Epoch, after.Epoch)
	}

	// A post-restart write must stamp a strictly greater epoch (no reset to 0).
	if _, err := c.Node(0).KV.Set("ep2", []byte("v2")); err != nil {
		t.Fatalf("set after restart: %v", err)
	}
	next, err := c.Node(0).KV.Get("ep2")
	if err != nil {
		t.Fatalf("get ep2: %v", err)
	}
	if next.Epoch <= before.Epoch {
		t.Fatalf("post-restart epoch must continue monotonically: before=%d next=%d", before.Epoch, next.Epoch)
	}
}

// TestE2E_SharedRaftCarriesBoth proves the single raft multiplexes a non-kv
// (primary) command alongside kv commands across all nodes.
func TestE2E_SharedRaftCarriesBoth(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node raft test")
	}
	c := NewCluster(t, 3)
	leader := c.Leader()

	// Untagged primary command (e.g. a registry op) through the same raft.
	if _, err := leader.Raft.Apply([]byte("registry-op"), 3*time.Second); err != nil {
		t.Fatalf("primary apply: %v", err)
	}
	if _, err := leader.KV.Set("kv", []byte("v")); err != nil {
		t.Fatalf("kv set: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for _, n := range c.Nodes() {
		for {
			_, kvErr := n.KV.Get("kv")
			if n.Primary.Count() > 0 && kvErr == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("node %s: primary=%d kvErr=%v", n.ID, n.Primary.Count(), kvErr)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}
