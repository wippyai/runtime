// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"sort"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/cluster"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/runtime/security"
)

type nodeInfo struct {
	meta    cluster.NodeMeta
	id      string
	addr    string
	isLocal bool
}

// collectNodes assembles a deduplicated, sorted snapshot of the cluster
// view derived from the relay node and cluster.Membership in ctx. The
// local node sorts first; remaining nodes by id. Returns nil when no
// information is reachable. Shared by system.cluster.members.
func collectNodes(ctx context.Context) []nodeInfo {
	localNode := relay.GetNode(ctx)
	membership := cluster.GetMembership(ctx)

	nodes := make([]nodeInfo, 0, 1)
	seen := make(map[string]int)

	add := func(info nodeInfo) {
		if info.id == "" {
			return
		}
		if existing, ok := seen[info.id]; ok {
			if info.isLocal {
				nodes[existing].isLocal = true
			}
			if nodes[existing].addr == "" && info.addr != "" {
				nodes[existing].addr = info.addr
			}
			if nodes[existing].meta == nil && info.meta != nil {
				nodes[existing].meta = info.meta
			}
			return
		}
		seen[info.id] = len(nodes)
		nodes = append(nodes, info)
	}

	if membership != nil {
		local := membership.LocalNode()
		if local.ID == "" && localNode != nil {
			local.ID = localNode.ID()
		}
		add(nodeInfo{
			id:      local.ID,
			addr:    local.Addr,
			meta:    local.Meta,
			isLocal: true,
		})

		for _, node := range membership.Nodes() {
			add(nodeInfo{
				id:      node.ID,
				addr:    node.Addr,
				meta:    node.Meta,
				isLocal: local.ID != "" && node.ID == local.ID,
			})
		}
	} else if localNode != nil {
		add(nodeInfo{
			id:      localNode.ID(),
			isLocal: true,
		})
	}

	if len(nodes) == 0 {
		return nil
	}

	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].isLocal != nodes[j].isLocal {
			return nodes[i].isLocal
		}
		return nodes[i].id < nodes[j].id
	})
	return nodes
}

// pushNodes encodes a []nodeInfo as a Lua table of node info records.
func pushNodes(l *lua.LState, nodes []nodeInfo) *lua.LTable {
	result := l.CreateTable(len(nodes), 0)
	for i, node := range nodes {
		t := l.CreateTable(0, 4)
		t.RawSetString("id", lua.LString(node.id))
		t.RawSetString("is_local", lua.LBool(node.isLocal))
		if node.addr != "" {
			t.RawSetString("addr", lua.LString(node.addr))
		}
		if node.meta != nil {
			meta := l.CreateTable(0, len(node.meta))
			for key, val := range node.meta {
				meta.RawSetString(key, lua.LString(val))
			}
			t.RawSetString("meta", meta)
		}
		result.RawSetInt(i+1, t)
	}
	return result
}

// localAddr returns the addr of the local node, derived from
// cluster.Membership when available. Returns "" when unknown.
func localAddr(ctx context.Context) string {
	if m := cluster.GetMembership(ctx); m != nil {
		return m.LocalNode().Addr
	}
	return ""
}

func nodeID(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "node", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on node").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	node := relay.GetNode(l.Context())
	if node == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "relay node not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	l.Push(lua.LString(node.ID()))
	l.Push(lua.LNil)
	return 2
}

func nodeAddr(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "node", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on node").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	addr := localAddr(l.Context())
	if addr == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "local node address not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	l.Push(lua.LString(addr))
	l.Push(lua.LNil)
	return 2
}

func nodeRole(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "node", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on node").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	svc := raftapi.GetService(l.Context())
	role := localRoleFromService(svc, localNodeID(l))

	l.Push(lua.LString(role))
	l.Push(lua.LNil)
	return 2
}
