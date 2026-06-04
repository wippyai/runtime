// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	systemkv "github.com/wippyai/runtime/system/kv"
	"go.uber.org/zap"
)

// TestE2E_LockMutualExclusionAndNodeReap proves distributed locks over the shared
// kv: a lock acquired on one node replicates everywhere, blocks others, and is
// auto-released cluster-wide when its holder's node dies (ReapNode), preventing
// a permanent deadlock.
func TestE2E_LockMutualExclusionAndNodeReap(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node lock test")
	}
	c := NewCluster(t, 3)
	locks := make([]*systemkv.LockService, len(c.Nodes()))
	for i, n := range c.Nodes() {
		locks[i] = systemkv.NewLockService(n.KV, nil, n.ID, zap.NewNop())
	}

	holder := pid.PID{Node: "node-2", Host: "h", UniqID: "job"}

	// Acquire on node-2 (forwarded to leader, committed, replicated).
	if ok, err := locks[2].Acquire("joblock", holder); err != nil || !ok {
		t.Fatalf("acquire: ok=%v err=%v", ok, err)
	}
	// Replicated: every node sees the holder and a second acquirer is blocked.
	for i := range locks {
		deadline := time.Now().Add(5 * time.Second)
		for {
			if h, ok, _ := locks[i].Holder("joblock"); ok && h.String() == holder.String() {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("node %d never saw lock holder", i)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	other := pid.PID{Node: "node-0", Host: "h", UniqID: "other"}
	if ok, _ := locks[0].Acquire("joblock", other); ok {
		t.Fatalf("a held lock must block another acquirer")
	}

	// Kill the holder's node; a survivor reaps node-2's locks.
	c.Kill(2)
	c.WaitLeader(10 * time.Second)
	survivor := 0
	if c.Node(0).Raft == nil { // defensive; node-0 is alive here
		survivor = 1
	}
	locks[survivor].ReapNode("node-2")

	// The lock is now free cluster-wide; another process can take it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		ok, err := locks[survivor].Acquire("joblock", other)
		if err == nil && ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lock not released after holder node died (deadlock): ok=%v err=%v", ok, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
