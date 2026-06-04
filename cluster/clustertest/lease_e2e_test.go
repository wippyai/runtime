// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"context"
	"errors"
	"testing"
	"time"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// TestE2E_LeaseExpiryFromFollower proves a lease granted on a FOLLOWER (the grant
// + set-with-lease forwarded to the leader) auto-expires cluster-wide: the leader
// re-arms it from replicated state and the revoke replicates to every node. This
// is the substrate behind _sys:lease and TTL'd store.Set.
func TestE2E_LeaseExpiryFromFollower(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node lease test")
	}
	c := NewCluster(t, 3)
	f := c.Follower()

	lease, err := f.KV.GrantLease(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("grant on follower: %v", err)
	}
	if _, err := f.KV.SetWithLease("ek", []byte("v"), lease.ID()); err != nil {
		t.Fatalf("set with lease: %v", err)
	}

	// Replicated to all nodes.
	for _, n := range c.Nodes() {
		waitKV(t, n, "ek", "v", 5*time.Second)
	}

	// Auto-expires on every node (leader sweep proposes a revoke; replicated).
	deadline := time.Now().Add(8 * time.Second)
	for _, n := range c.Nodes() {
		for {
			if _, err := n.KV.Get("ek"); errors.Is(err, kvapi.ErrKeyNotFound) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("node %s: leased key did not expire cluster-wide", n.ID)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}
