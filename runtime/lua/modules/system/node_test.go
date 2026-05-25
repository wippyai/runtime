// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/cluster"
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

func (s *stubMembership) Nodes() []cluster.NodeInfo   { return s.peers }
func (s *stubMembership) LocalNode() cluster.NodeInfo { return s.local }

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

func TestNodesList_LocalNodeOnly(t *testing.T) {
	l, ctx := newNodeTestState(t)
	ctx = relay.WithNode(ctx, &stubRelayNode{id: "solo"})
	l.SetContext(ctx)

	err := l.DoString(`
		local nodes, err = system.nodes.list()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(#nodes == 1, "expected 1 node, got " .. #nodes)
		assert(nodes[1].id == "solo", "expected solo, got " .. tostring(nodes[1].id))
		assert(nodes[1].is_local == true, "expected is_local true")
	`)
	require.NoError(t, err)
}

func TestNodesList_WithMembership(t *testing.T) {
	l, ctx := newNodeTestState(t)
	ctx = relay.WithNode(ctx, &stubRelayNode{id: "node-1"})
	ctx = cluster.WithMembership(ctx, &stubMembership{
		local: cluster.NodeInfo{ID: "node-1", Addr: "10.0.0.1:7946", Meta: cluster.NodeMeta{"role": "leader"}},
		peers: []cluster.NodeInfo{
			{ID: "node-2", Addr: "10.0.0.2:7946", Meta: cluster.NodeMeta{"role": "follower"}},
			{ID: "node-3", Addr: "10.0.0.3:7946"},
		},
	})
	l.SetContext(ctx)

	err := l.DoString(`
		local nodes, err = system.nodes.list()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(#nodes == 3, "expected 3 nodes, got " .. #nodes)
		assert(nodes[1].is_local == true, "first node must be local")
		assert(nodes[1].id == "node-1", "first id mismatch: " .. tostring(nodes[1].id))
		assert(nodes[1].addr == "10.0.0.1:7946", "addr mismatch")
		assert(nodes[1].meta.role == "leader", "meta role mismatch")
		assert(nodes[2].is_local == false, "node-2 must not be local")
		assert(nodes[2].id == "node-2", "second id mismatch")
		assert(nodes[3].id == "node-3", "third id mismatch")
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

func TestNodesList_PermissionDenied(t *testing.T) {
	l := lua.NewState()
	t.Cleanup(func() { l.Close() })

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx = security.SetStrictMode(ctx, true)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	err := l.DoString(`
		local nodes, err = system.nodes.list()
		assert(nodes == nil, "expected nil under strict security")
		assert(err ~= nil, "expected permission-denied error")
	`)
	require.NoError(t, err)
}
