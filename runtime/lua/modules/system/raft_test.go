// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	raftapi "github.com/wippyai/runtime/api/raft"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/security"
)

func newRaftTestState(t *testing.T) (*lua.LState, context.Context) {
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

func TestRaftIsLeader(t *testing.T) {
	cases := []struct {
		name   string
		leader bool
	}{
		{"leader", true},
		{"follower", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, ctx := newRaftTestState(t)
			ctx = raftapi.WithService(ctx, &fakeRaftService{isLeader: tc.leader})
			l.SetContext(ctx)

			err := l.DoString(`
				local v, err = system.raft.is_leader()
				assert(err == nil, "unexpected error: " .. tostring(err))
				return v
			`)
			require.NoError(t, err)
			require.Equal(t, lua.LBool(tc.leader), l.Get(-1))
		})
	}
}

func TestRaftIsLeader_NoService(t *testing.T) {
	l, _ := newRaftTestState(t)

	err := l.DoString(`
		local v, err = system.raft.is_leader()
		assert(v == nil, "expected nil when raft unavailable")
		assert(err ~= nil, "expected error when raft unavailable")
	`)
	require.NoError(t, err)
}

func TestRaftIsMember(t *testing.T) {
	cases := []struct {
		name    string
		localID string
		servers []raftapi.Server
		want    bool
	}{
		{"voter member", "node-1", []raftapi.Server{{ID: "node-1", IsVoter: true}}, true},
		{"nonvoter member", "node-1", []raftapi.Server{{ID: "node-1", IsVoter: false}}, true},
		{"not in config", "node-1", []raftapi.Server{{ID: "node-2", IsVoter: true}}, false},
		{"empty config", "node-1", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, ctx := newRaftTestState(t)
			ctx = relay.WithNode(ctx, &stubRelayNode{id: tc.localID})
			ctx = raftapi.WithService(ctx, &fakeRaftService{servers: tc.servers})
			l.SetContext(ctx)

			err := l.DoString(`
				local v, err = system.raft.is_member()
				assert(err == nil, "unexpected error: " .. tostring(err))
				return v
			`)
			require.NoError(t, err)
			require.Equal(t, lua.LBool(tc.want), l.Get(-1))
		})
	}
}

func TestRaftRole(t *testing.T) {
	cases := []struct {
		name     string
		localID  string
		isLeader bool
		servers  []raftapi.Server
		want     string
	}{
		{"leader", "node-1", true, []raftapi.Server{{ID: "node-1", IsVoter: true}}, "leader"},
		{"voter", "node-1", false, []raftapi.Server{{ID: "node-1", IsVoter: true}}, "voter"},
		{"standby", "node-1", false, []raftapi.Server{{ID: "node-1", IsVoter: false}}, "standby"},
		{"non-member", "node-1", false, []raftapi.Server{{ID: "node-2", IsVoter: true}}, "non-member"},
		{"non-member empty", "node-1", false, nil, "non-member"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, ctx := newRaftTestState(t)
			ctx = relay.WithNode(ctx, &stubRelayNode{id: tc.localID})
			ctx = raftapi.WithService(ctx, &fakeRaftService{isLeader: tc.isLeader, servers: tc.servers})
			l.SetContext(ctx)

			err := l.DoString(`
				local v, err = system.raft.role()
				assert(err == nil, "unexpected error: " .. tostring(err))
				return v
			`)
			require.NoError(t, err)
			require.Equal(t, lua.LString(tc.want), l.Get(-1))
		})
	}
}

func TestRaftRole_NoService(t *testing.T) {
	l, _ := newRaftTestState(t)

	err := l.DoString(`
		local v, err = system.raft.role()
		assert(v == nil, "expected nil when raft unavailable")
		assert(err ~= nil, "expected error when raft unavailable")
	`)
	require.NoError(t, err)
}

func TestRaftTerm(t *testing.T) {
	l, ctx := newRaftTestState(t)
	ctx = raftapi.WithService(ctx, &fakeRaftService{stats: map[string]string{"term": "42"}})
	l.SetContext(ctx)

	err := l.DoString(`
		local v, err = system.raft.term()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(v == 42, "expected 42, got " .. tostring(v))
	`)
	require.NoError(t, err)
}

func TestRaftTerm_MissingStat(t *testing.T) {
	l, ctx := newRaftTestState(t)
	ctx = raftapi.WithService(ctx, &fakeRaftService{stats: map[string]string{}})
	l.SetContext(ctx)

	err := l.DoString(`
		local v, err = system.raft.term()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(v == 0, "expected 0 default, got " .. tostring(v))
	`)
	require.NoError(t, err)
}

func TestRaftCommitIndex(t *testing.T) {
	l, ctx := newRaftTestState(t)
	ctx = raftapi.WithService(ctx, &fakeRaftService{commitIndex: 1234})
	l.SetContext(ctx)

	err := l.DoString(`
		local v, err = system.raft.commit_index()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(v == 1234, "expected 1234, got " .. tostring(v))
	`)
	require.NoError(t, err)
}

func TestRaftStats(t *testing.T) {
	l, ctx := newRaftTestState(t)
	ctx = raftapi.WithService(ctx, &fakeRaftService{stats: map[string]string{
		"term":         "7",
		"commit_index": "99",
		"state":        "Follower",
	}})
	l.SetContext(ctx)

	err := l.DoString(`
		local s, err = system.raft.stats()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(type(s) == "table", "expected table")
		assert(s.term == "7", "term mismatch: " .. tostring(s.term))
		assert(s.commit_index == "99", "commit_index mismatch: " .. tostring(s.commit_index))
		assert(s.state == "Follower", "state mismatch: " .. tostring(s.state))
	`)
	require.NoError(t, err)
}

func TestRaftStats_NoService(t *testing.T) {
	l, _ := newRaftTestState(t)

	err := l.DoString(`
		local s, err = system.raft.stats()
		assert(s == nil, "expected nil when raft unavailable")
		assert(err ~= nil, "expected error when raft unavailable")
	`)
	require.NoError(t, err)
}

func TestRaft_PermissionDenied(t *testing.T) {
	cases := []struct {
		call string
	}{
		{"system.raft.is_leader()"},
		{"system.raft.is_member()"},
		{"system.raft.role()"},
		{"system.raft.term()"},
		{"system.raft.commit_index()"},
		{"system.raft.stats()"},
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
