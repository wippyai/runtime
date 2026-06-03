// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"context"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/topology/namereg/kvbacked"
)

// TestE2E_KVRegistry_ConsistentReplicates proves a Consistent-scope name
// registered through the kv-backed registry on a follower forwards to the
// leader, gets a non-zero raft-index epoch, and is resolvable on every node.
func TestE2E_KVRegistry_ConsistentReplicates(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node registry test")
	}
	c := NewCluster(t, 3)
	f := c.Follower()

	reg := kvbacked.NewService(f.KV, f.ID, nil, nil)
	p := pid.PID{Node: f.ID, Host: "proc", UniqID: "a"}

	out, err := reg.RegisterScope(context.Background(), "svc", p, globalapi.Consistent)
	if err != nil {
		t.Fatalf("register on follower: %v", err)
	}
	if out.Epoch == 0 {
		t.Fatalf("epoch must be the raft index on the raft backend, got 0")
	}

	for _, n := range c.Nodes() {
		nreg := kvbacked.NewService(n.KV, n.ID, nil, nil)
		waitLookup(t, nreg, "svc", p, 5*time.Second)
	}

	// Conflict from another node is rejected cluster-wide (first-write-wins).
	other := pid.PID{Node: c.Leader().ID, Host: "proc", UniqID: "b"}
	leaderReg := kvbacked.NewService(c.Leader().KV, c.Leader().ID, nil, nil)
	if _, err := leaderReg.Register(context.Background(), "svc", other); err == nil {
		t.Fatalf("conflicting register must fail")
	}

	// Remove on a follower clears it everywhere.
	if err := reg.Remove(context.Background(), p); err != nil {
		t.Fatalf("remove: %v", err)
	}
	for _, n := range c.Nodes() {
		nreg := kvbacked.NewService(n.KV, n.ID, nil, nil)
		waitNoLookup(t, nreg, "svc", 5*time.Second)
	}
}

func waitLookup(t *testing.T, r *kvbacked.Service, name string, want pid.PID, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		res, err := r.Lookup(context.Background(), name)
		if err == nil && res.Found && res.PID.String() == want.String() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("lookup %q did not resolve to %s in time (err=%v res=%+v)", name, want, err, res)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func waitNoLookup(t *testing.T, r *kvbacked.Service, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		res, err := r.Lookup(context.Background(), name)
		if err == nil && !res.Found {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("lookup %q still resolves after remove", name)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
