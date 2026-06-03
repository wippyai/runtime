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

// TestE2E_KVRegistry_StrongPromotes proves a Strong-scope registration opened on
// a follower collects an ack from every node (each ack is a raft-replicated kv
// write — no ack relay), the leader promotes atomically, and the active name is
// resolvable on every node.
func TestE2E_KVRegistry_StrongPromotes(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node strong test")
	}
	c := NewCluster(t, 3)

	var members []pid.NodeID
	for _, n := range c.Nodes() {
		members = append(members, n.ID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	regs := make(map[string]*kvbacked.Service, len(c.Nodes()))
	for _, n := range c.Nodes() {
		node := n
		reg := kvbacked.NewService(node.KV, node.ID, nil, nil)
		reg.ConfigureStrong(kvbacked.StrongDeps{
			Membership: func() []pid.NodeID { return members },
			IsLeader:   func() bool { return node.Raft.IsLeader() },
			Deadline:   8 * time.Second,
		})
		if err := reg.StartReconciler(ctx); err != nil {
			t.Fatalf("start reconciler on %s: %v", node.ID, err)
		}
		regs[node.ID] = reg
	}

	f := c.Follower()
	p := pid.PID{Node: f.ID, Host: "proc", UniqID: "s1"}
	out, err := regs[f.ID].RegisterScope(context.Background(), "strongsvc", p, globalapi.Strong)
	if err != nil {
		t.Fatalf("strong register on follower: %v", err)
	}
	if out.State != globalapi.RegisterStateActive || out.PID.String() != p.String() {
		t.Fatalf("strong outcome: %+v", out)
	}

	for _, n := range c.Nodes() {
		waitLookup(t, regs[n.ID], "strongsvc", p, 5*time.Second)
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
