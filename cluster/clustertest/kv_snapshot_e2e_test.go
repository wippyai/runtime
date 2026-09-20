// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"context"
	"errors"
	"testing"
	"time"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// TestE2E_KVAuthoritySnapshot_UsesOneLeaderPublication proves a real three-node
// Raft path with an in-process relay: a follower reads keys from the barriered
// immutable KV publication and receives one KV-domain revision.
func TestE2E_KVAuthoritySnapshot_UsesOneLeaderPublication(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node authority snapshot test")
	}
	c := NewCluster(t, 3)
	leader := c.Leader()
	if _, err := leader.KV.Set("authority/a", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := leader.KV.Set("authority/b", []byte("b")); err != nil {
		t.Fatal(err)
	}
	follower := c.Follower()
	snap, err := follower.KV.ReadAuthoritySnapshot(context.Background(), []string{"authority/b", "missing", "authority/a"})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Revision == 0 {
		t.Fatal("authority snapshot has no KV publication revision")
	}
	if string(snap.Entries["authority/a"].Value) != "a" || string(snap.Entries["authority/b"].Value) != "b" {
		t.Fatalf("snapshot entries = %+v", snap.Entries)
	}
	if _, ok := snap.Entries["missing"]; ok {
		t.Fatal("missing key appeared in authority snapshot")
	}
	var _ kvapi.AuthoritySnapshotReader = follower.KV
}

func TestE2E_KVAuthoritySnapshot_PartitionFailsWithoutStaleFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node authority snapshot test")
	}
	c := NewCluster(t, 3)
	if _, err := c.Leader().KV.Set("authority/partition", []byte("current")); err != nil {
		t.Fatal(err)
	}
	var followerIdx = -1
	for i, n := range c.Nodes() {
		if !n.Raft.IsLeader() {
			followerIdx = i
			break
		}
	}
	if followerIdx < 0 {
		t.Fatal("no follower")
	}
	c.Partition(followerIdx)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.Node(followerIdx).KV.ReadAuthoritySnapshot(ctx, []string{"authority/partition"}); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("partitioned authority read = %v, want prompt routing failure", err)
	}
	c.Heal(followerIdx)
	snap, err := c.Node(followerIdx).KV.ReadAuthoritySnapshot(context.Background(), []string{"authority/partition"})
	if err != nil || string(snap.Entries["authority/partition"].Value) != "current" {
		t.Fatalf("healed authority read = %+v, %v", snap, err)
	}
}
