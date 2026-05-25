// SPDX-License-Identifier: MPL-2.0

package system

import (
	"sort"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/runtime/security"
)

type nodeInfo struct {
	meta    cluster.NodeMeta
	id      string
	addr    string
	isLocal bool
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

func nodesList(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "nodes", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on nodes").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	localNode := relay.GetNode(l.Context())
	membership := cluster.GetMembership(l.Context())

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
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "cluster node information not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].isLocal != nodes[j].isLocal {
			return nodes[i].isLocal
		}
		return nodes[i].id < nodes[j].id
	})

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

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}
