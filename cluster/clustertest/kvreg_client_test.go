// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"context"
	"testing"
	"time"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/eventbus"
	systemkv "github.com/wippyai/runtime/system/kv"
	"github.com/wippyai/runtime/system/topology/namereg/kvbacked"
	"go.uber.org/zap"
)

// clientSubmitter models a registry non-member (role=client): it runs no raft
// node, so every kv op is "not leader" and the RaftEngine forwards it to the
// leader over the relay. Leader resolves the current leader id dynamically.
type clientSubmitter struct {
	leader func() string
}

func (c clientSubmitter) Apply([]byte, time.Duration) (*raftapi.ApplyResponse, error) {
	return nil, raftapi.ErrNotLeader
}
func (c clientSubmitter) IsLeader() bool { return false }
func (c clientSubmitter) Leader() (raftapi.ServerID, raftapi.ServerAddress, error) {
	return c.leader(), "", nil
}
func (c clientSubmitter) Barrier(time.Duration) error { return raftapi.ErrNotLeader }
func (c clientSubmitter) CommitIndex() uint64         { return 0 }

// newClientRegistry builds a non-member client registry: an empty local kv whose
// reads/writes all forward to the leader over the harness relay, plus a kv-backed
// registry in non-member mode (cold-miss forward-resolve enabled).
func (c *Cluster) newClientRegistry(t *testing.T, id string) *kvbacked.Service {
	t.Helper()
	return c.newClientRegistryTarget(t, id, func() string {
		if l := c.Leader(); l != nil {
			return l.ID
		}
		return ""
	})
}

// newClientRegistryTarget builds a non-member client whose ops forward to the
// node returned by target — which need NOT be the leader. When target is a
// follower, the follower re-forwards to the leader, exercising the server-side
// re-forwarding path a production client (which cannot call raft.Leader()) needs.
func (c *Cluster) newClientRegistryTarget(t *testing.T, id string, target func() string) *kvbacked.Service {
	t.Helper()
	bus := eventbus.NewBus()
	fsm := systemkv.NewRaftFSM(bus)
	eng := systemkv.NewRaftEngine(
		clientSubmitter{leader: target},
		fsm, bus, id, c.router, zap.NewNop())
	c.router.register(id, systemkv.KVRaftHostID, eng)
	if err := eng.Start(context.Background()); err != nil {
		t.Fatalf("start client engine: %v", err)
	}
	t.Cleanup(func() { _ = eng.Stop() })
	reg := kvbacked.NewService(eng, id, nil, nil)
	reg.SetNonMember(func() bool { return true })
	return reg
}

// TestE2E_KVRegistry_ClientResolves proves a registry non-member (role=client,
// no raft FSM) resolves a name registered on a member via the cold-miss
// forward-resolve, and can itself register a name (forwarded to the leader) that
// members then resolve. This is the non-member resolution path end-to-end over
// the real relay.
func TestE2E_KVRegistry_ClientResolves(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node client-resolution test")
	}
	c := NewCluster(t, 3)
	member := c.Leader()
	memReg := kvbacked.NewService(member.KV, member.ID, nil, nil)

	p := pid.PID{Node: member.ID, Host: "proc", UniqID: "m"}
	if _, err := memReg.RegisterScope(context.Background(), "svc", p, globalapi.Consistent); err != nil {
		t.Fatalf("member register: %v", err)
	}

	client := c.newClientRegistry(t, "client-0")

	// The non-member resolves the member-registered name via forward-resolve.
	res, err := client.Lookup(context.Background(), "svc")
	if err != nil || !res.Found || res.PID.String() != p.String() {
		t.Fatalf("client cold-miss forward-resolve failed: res=%+v err=%v", res, err)
	}

	// The non-member registers its own name (forwarded to the leader); members
	// then resolve it from their replicated state.
	cp := pid.PID{Node: "client-0", Host: "proc", UniqID: "c"}
	if _, err := client.Register(context.Background(), "csvc", cp); err != nil {
		t.Fatalf("client register: %v", err)
	}
	waitLookup(t, memReg, "csvc", cp, 5*time.Second)
}

// TestE2E_KVRegistry_ClientReforwardsViaFollower proves a non-member that targets
// a FOLLOWER (the realistic production case — a client picks any member from
// gossip, not knowing which is leader) still has its register/lookup applied:
// the follower re-forwards the op to the leader over the relay. This is the
// server-side re-forwarding path that lets a clientless-of-raft node reach the
// leader without calling raft.Leader() itself.
func TestE2E_KVRegistry_ClientReforwardsViaFollower(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node client re-forwarding test")
	}
	c := NewCluster(t, 3)
	c.WaitLeader(5 * time.Second)
	follower := c.Follower()
	if follower == nil {
		t.Fatal("no follower")
	}
	followerID := follower.ID

	member := c.Leader()
	memReg := kvbacked.NewService(member.KV, member.ID, nil, nil)
	p := pid.PID{Node: member.ID, Host: "proc", UniqID: "m"}
	if _, err := memReg.RegisterScope(context.Background(), "fsvc", p, globalapi.Consistent); err != nil {
		t.Fatalf("member register: %v", err)
	}

	// The client always targets the follower; the follower re-forwards to the leader.
	client := c.newClientRegistryTarget(t, "client-rf", func() string { return followerID })

	res, err := client.Lookup(context.Background(), "fsvc")
	if err != nil || !res.Found || res.PID.String() != p.String() {
		t.Fatalf("client read re-forward via follower failed: res=%+v err=%v", res, err)
	}

	cp := pid.PID{Node: "client-rf", Host: "proc", UniqID: "c"}
	if _, err := client.Register(context.Background(), "rfsvc", cp); err != nil {
		t.Fatalf("client write re-forward via follower failed: %v", err)
	}
	waitLookup(t, memReg, "rfsvc", cp, 5*time.Second)
}
