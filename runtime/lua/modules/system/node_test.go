// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/cluster"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	ctxapi "github.com/wippyai/runtime/api/context"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/security"
)

type stubRelayNode struct {
	id pidapi.NodeID
}

func (s *stubRelayNode) ID() pidapi.NodeID         { return s.id }
func (s *stubRelayNode) Send(*relay.Package) error { return nil }
func (s *stubRelayNode) Attach(pidapi.PID, chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (s *stubRelayNode) Detach(pidapi.PID)                                {}
func (s *stubRelayNode) RegisterHost(pidapi.HostID, relay.Receiver) error { return nil }
func (s *stubRelayNode) UnregisterHost(pidapi.HostID)                     {}
func (s *stubRelayNode) GetHost(pidapi.HostID) (relay.Receiver, bool)     { return nil, false }

type stubMembership struct {
	local cluster.NodeInfo
	peers []cluster.NodeInfo
}

func (s *stubMembership) Nodes() []cluster.NodeInfo    { return s.peers }
func (s *stubMembership) LocalNode() cluster.NodeInfo  { return s.local }
func (s *stubMembership) UpdateMeta(map[string]string) {}

func newNodeTestState(t *testing.T) (*lua.LState, context.Context) {
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

func TestNodeID_WithRelayNode(t *testing.T) {
	l, ctx := newNodeTestState(t)
	ctx = relay.WithNode(ctx, &stubRelayNode{id: "node-alpha"})
	l.SetContext(ctx)

	err := l.DoString(`
		local id, err = system.node.id()
		assert(err == nil, "expected nil error, got: " .. tostring(err))
		assert(id == "node-alpha", "expected node-alpha, got: " .. tostring(id))
	`)
	require.NoError(t, err)
}

func TestNodeID_WithoutRelayNode(t *testing.T) {
	l, _ := newNodeTestState(t)

	err := l.DoString(`
		local id, err = system.node.id()
		assert(id == nil, "expected nil id")
		assert(err ~= nil, "expected error when relay node missing")
	`)
	require.NoError(t, err)
}

func TestNodeID_PermissionDenied(t *testing.T) {
	l := lua.NewState()
	t.Cleanup(func() { l.Close() })

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx = security.SetStrictMode(ctx, true)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	err := l.DoString(`
		local id, err = system.node.id()
		assert(id == nil, "expected nil id under strict security")
		assert(err ~= nil, "expected permission-denied error")
	`)
	require.NoError(t, err)
}

func TestNodeAddr_WithMembership(t *testing.T) {
	l, ctx := newNodeTestState(t)
	ctx = cluster.WithMembership(ctx, &stubMembership{
		local: cluster.NodeInfo{ID: "node-1", Addr: "10.0.0.1:7946"},
	})
	l.SetContext(ctx)

	err := l.DoString(`
		local addr, err = system.node.addr()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(addr == "10.0.0.1:7946", "addr mismatch: " .. tostring(addr))
	`)
	require.NoError(t, err)
}

func TestNodeAddr_WithoutMembership(t *testing.T) {
	l, _ := newNodeTestState(t)

	err := l.DoString(`
		local addr, err = system.node.addr()
		assert(addr == nil, "expected nil when membership absent")
		assert(err ~= nil, "expected error when membership absent")
	`)
	require.NoError(t, err)
}

func TestNodeRole_NoRaft(t *testing.T) {
	l, _ := newNodeTestState(t)

	err := l.DoString(`
		local role, err = system.node.role()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(role == "non-member", "expected non-member, got " .. tostring(role))
	`)
	require.NoError(t, err)
}

func TestNodeRole_Leader(t *testing.T) {
	l, ctx := newNodeTestState(t)
	ctx = relay.WithNode(ctx, &stubRelayNode{id: "node-1"})
	ctx = raftapi.WithService(ctx, &fakeRaftService{isLeader: true, servers: []raftapi.Server{{ID: "node-1", IsVoter: true}}})
	l.SetContext(ctx)

	err := l.DoString(`
		local role, err = system.node.role()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(role == "leader", "expected leader, got " .. tostring(role))
	`)
	require.NoError(t, err)
}

func TestNodeAddr_PermissionDenied(t *testing.T) {
	l := lua.NewState()
	t.Cleanup(func() { l.Close() })

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx = security.SetStrictMode(ctx, true)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	err := l.DoString(`
		local addr, err = system.node.addr()
		assert(addr == nil, "expected nil under strict security")
		assert(err ~= nil, "expected permission-denied error")
	`)
	require.NoError(t, err)
}

func TestNodeRole_PermissionDenied(t *testing.T) {
	l := lua.NewState()
	t.Cleanup(func() { l.Close() })

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx = security.SetStrictMode(ctx, true)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	err := l.DoString(`
		local role, err = system.node.role()
		assert(role == nil, "expected nil under strict security")
		assert(err ~= nil, "expected permission-denied error")
	`)
	require.NoError(t, err)
}
