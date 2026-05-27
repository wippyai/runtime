// SPDX-License-Identifier: MPL-2.0

package system

import (
	lua "github.com/wippyai/go-lua"
	raftapi "github.com/wippyai/runtime/api/raft"
	"github.com/wippyai/runtime/runtime/security"
)

func clusterMembers(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "cluster", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on cluster").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	nodes := collectNodes(l.Context())
	if nodes == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "cluster membership not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	l.Push(pushNodes(l, nodes))
	l.Push(lua.LNil)
	return 2
}

func clusterLeader(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "cluster", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on cluster").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	svc := raftapi.GetService(l.Context())
	if svc == nil {
		l.Push(lua.LString(""))
		l.Push(lua.LNil)
		return 2
	}
	id, _, err := svc.Leader()
	if err != nil {
		l.Push(lua.LString(""))
		l.Push(lua.LNil)
		return 2
	}
	l.Push(lua.LString(id))
	l.Push(lua.LNil)
	return 2
}

func clusterSize(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "cluster", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on cluster").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	nodes := collectNodes(l.Context())
	l.Push(lua.LNumber(len(nodes)))
	l.Push(lua.LNil)
	return 2
}
