// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/cluster"
	ctxapi "github.com/wippyai/runtime/api/context"
	raftapi "github.com/wippyai/runtime/api/raft"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/security"
)

// fakeRaftService is a minimal raftapi.Service stub for system.* tests.
type fakeRaftService struct {
	leaderErr   error
	stats       map[string]string
	leaderID    raftapi.ServerID
	leaderAddr  raftapi.ServerAddress
	servers     []raftapi.Server
	commitIndex uint64
	isLeader    bool
}

func (f *fakeRaftService) Apply(_ []byte, _ time.Duration) (*raftapi.ApplyResponse, error) {
	return nil, nil
}
func (f *fakeRaftService) Leader() (raftapi.ServerID, raftapi.ServerAddress, error) {
	return f.leaderID, f.leaderAddr, f.leaderErr
}
func (f *fakeRaftService) IsLeader() bool        { return f.isLeader }
func (f *fakeRaftService) LeaderCh() <-chan bool { return nil }
func (f *fakeRaftService) State() raftapi.State {
	if f.isLeader {
		return raftapi.Leader
	}
	return raftapi.Follower
}
func (f *fakeRaftService) Barrier(_ time.Duration) error { return nil }
func (f *fakeRaftService) CommitIndex() uint64           { return f.commitIndex }
func (f *fakeRaftService) AddVoter(_ raftapi.ServerID, _ raftapi.ServerAddress, _ time.Duration) error {
	return nil
}
func (f *fakeRaftService) AddNonvoter(_ raftapi.ServerID, _ raftapi.ServerAddress, _ time.Duration) error {
	return nil
}
func (f *fakeRaftService) DemoteVoter(_ raftapi.ServerID, _ time.Duration) error  { return nil }
func (f *fakeRaftService) RemoveServer(_ raftapi.ServerID, _ time.Duration) error { return nil }
func (f *fakeRaftService) LeadershipTransfer(_ raftapi.ServerID, _ time.Duration) error {
	return nil
}
func (f *fakeRaftService) GetConfiguration() ([]raftapi.Server, error) {
	out := make([]raftapi.Server, len(f.servers))
	copy(out, f.servers)
	return out, nil
}
func (f *fakeRaftService) Stats() map[string]string { return f.stats }
func (f *fakeRaftService) LastContact() time.Time   { return time.Time{} }

func newClusterTestState(t *testing.T) (*lua.LState, context.Context) {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(func() { l.Close() })

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx = security.SetStrictMode(ctx, false)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)
	return l, ctx
}

func TestClusterMembers_WithMembership(t *testing.T) {
	l, ctx := newClusterTestState(t)
	ctx = relay.WithNode(ctx, &stubRelayNode{id: "node-1"})
	ctx = cluster.WithMembership(ctx, &stubMembership{
		local: cluster.NodeInfo{ID: "node-1", Addr: "10.0.0.1:7946"},
		peers: []cluster.NodeInfo{
			{ID: "node-2", Addr: "10.0.0.2:7946"},
		},
	})
	l.SetContext(ctx)

	err := l.DoString(`
		local members, err = system.cluster.members()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(#members == 2, "expected 2 members, got " .. #members)
		assert(members[1].id == "node-1", "first id mismatch: " .. tostring(members[1].id))
		assert(members[1].is_local == true, "first must be local")
		assert(members[1].addr == "10.0.0.1:7946", "addr mismatch")
		assert(members[2].id == "node-2", "second id mismatch")
		assert(members[2].is_local == false, "second must not be local")
	`)
	require.NoError(t, err)
}

func TestClusterMembers_Unavailable(t *testing.T) {
	l, _ := newClusterTestState(t)

	err := l.DoString(`
		local members, err = system.cluster.members()
		assert(members == nil, "expected nil members")
		assert(err ~= nil, "expected error when membership absent")
	`)
	require.NoError(t, err)
}

func TestClusterLeader_KnownID(t *testing.T) {
	l, ctx := newClusterTestState(t)
	ctx = raftapi.WithService(ctx, &fakeRaftService{leaderID: "node-9", leaderAddr: "10.0.0.9:7946"})
	l.SetContext(ctx)

	err := l.DoString(`
		local id, err = system.cluster.leader()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(id == "node-9", "leader mismatch: " .. tostring(id))
	`)
	require.NoError(t, err)
}

func TestClusterLeader_Unknown(t *testing.T) {
	l, ctx := newClusterTestState(t)
	ctx = raftapi.WithService(ctx, &fakeRaftService{leaderErr: raftapi.ErrNoLeader})
	l.SetContext(ctx)

	err := l.DoString(`
		local id, err = system.cluster.leader()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(id == "", "expected empty leader, got " .. tostring(id))
	`)
	require.NoError(t, err)
}

func TestClusterLeader_NoRaftService(t *testing.T) {
	l, _ := newClusterTestState(t)

	err := l.DoString(`
		local id, err = system.cluster.leader()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(id == "", "expected empty leader without raft")
	`)
	require.NoError(t, err)
}

func TestClusterSize(t *testing.T) {
	l, ctx := newClusterTestState(t)
	ctx = relay.WithNode(ctx, &stubRelayNode{id: "node-1"})
	ctx = cluster.WithMembership(ctx, &stubMembership{
		local: cluster.NodeInfo{ID: "node-1"},
		peers: []cluster.NodeInfo{
			{ID: "node-2"},
			{ID: "node-3"},
		},
	})
	l.SetContext(ctx)

	err := l.DoString(`
		local n, err = system.cluster.size()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(n == 3, "expected size 3, got " .. tostring(n))
	`)
	require.NoError(t, err)
}

func TestClusterSize_Empty(t *testing.T) {
	l, _ := newClusterTestState(t)

	err := l.DoString(`
		local n, err = system.cluster.size()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(n == 0, "expected size 0, got " .. tostring(n))
	`)
	require.NoError(t, err)
}

func TestCluster_PermissionDenied(t *testing.T) {
	cases := []struct {
		call string
	}{
		{"system.cluster.members()"},
		{"system.cluster.leader()"},
		{"system.cluster.size()"},
	}
	for _, tc := range cases {
		t.Run(tc.call, func(t *testing.T) {
			l := lua.NewState()
			t.Cleanup(func() { l.Close() })

			ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
			ctx = security.SetStrictMode(ctx, true)
			l.SetContext(ctx)

			tbl, _ := Module.Build()
			l.SetGlobal("system", tbl)

			err := l.DoString(`
				local v, err = ` + tc.call + `
				assert(v == nil, "expected nil under strict security")
				assert(err ~= nil, "expected permission-denied error")
			`)
			require.NoError(t, err)
		})
	}
}
