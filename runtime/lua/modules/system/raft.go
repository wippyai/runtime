// SPDX-License-Identifier: MPL-2.0

package system

import (
	"strconv"

	lua "github.com/wippyai/go-lua"
	raftapi "github.com/wippyai/runtime/api/raft"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/runtime/security"
)

func raftNotAvailable(l *lua.LState) int {
	l.Push(lua.LNil)
	l.Push(lua.NewLuaError(l, "raft not available").WithKind(lua.Internal).WithRetryable(false))
	return 2
}

func raftIsLeader(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "raft", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on raft").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	svc := raftapi.GetService(l.Context())
	if svc == nil {
		return raftNotAvailable(l)
	}

	l.Push(lua.LBool(svc.IsLeader()))
	l.Push(lua.LNil)
	return 2
}

func raftIsMember(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "raft", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on raft").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	svc := raftapi.GetService(l.Context())
	if svc == nil {
		return raftNotAvailable(l)
	}

	l.Push(lua.LBool(localIsMember(l, svc)))
	l.Push(lua.LNil)
	return 2
}

func raftRole(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "raft", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on raft").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	svc := raftapi.GetService(l.Context())
	if svc == nil {
		return raftNotAvailable(l)
	}

	l.Push(lua.LString(localRoleFromService(svc, localNodeID(l))))
	l.Push(lua.LNil)
	return 2
}

func raftTerm(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "raft", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on raft").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	svc := raftapi.GetService(l.Context())
	if svc == nil {
		return raftNotAvailable(l)
	}

	stats := svc.Stats()
	var term uint64
	if v, err := strconv.ParseUint(stats["term"], 10, 64); err == nil {
		term = v
	}
	l.Push(lua.LNumber(term))
	l.Push(lua.LNil)
	return 2
}

func raftCommitIndex(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "raft", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on raft").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	svc := raftapi.GetService(l.Context())
	if svc == nil {
		return raftNotAvailable(l)
	}

	l.Push(lua.LNumber(svc.CommitIndex()))
	l.Push(lua.LNil)
	return 2
}

func raftStats(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "raft_stats", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on raft_stats").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	svc := raftapi.GetService(l.Context())
	if svc == nil {
		return raftNotAvailable(l)
	}

	stats := svc.Stats()
	t := l.CreateTable(0, len(stats))
	for k, v := range stats {
		t.RawSetString(k, lua.LString(v))
	}
	l.Push(t)
	l.Push(lua.LNil)
	return 2
}

// localNodeID returns the local NodeID from relay context, or "" when
// unavailable. Used to match against raft configuration entries.
func localNodeID(l *lua.LState) string {
	if n := relay.GetNode(l.Context()); n != nil {
		return string(n.ID())
	}
	return ""
}

// localIsMember reports whether the local node appears in the committed
// Raft configuration as voter or non-voter. Pure local read.
func localIsMember(l *lua.LState, svc raftapi.Service) bool {
	if svc == nil {
		return false
	}
	id := localNodeID(l)
	if id == "" {
		return false
	}
	servers, err := svc.GetConfiguration()
	if err != nil {
		return false
	}
	for _, s := range servers {
		if s.ID == id {
			return true
		}
	}
	return false
}

// localRoleFromService composes "leader" | "voter" | "standby" |
// "non-member" from IsLeader plus the local suffrage in the committed
// configuration. Pure local read. Returns "non-member" when svc is nil.
func localRoleFromService(svc raftapi.Service, args ...string) string {
	if svc == nil {
		return "non-member"
	}
	if svc.IsLeader() {
		return "leader"
	}
	var id string
	if len(args) > 0 {
		id = args[0]
	}
	if id == "" {
		return "non-member"
	}
	servers, err := svc.GetConfiguration()
	if err != nil {
		return "non-member"
	}
	for _, s := range servers {
		if s.ID == id {
			if s.IsVoter {
				return "voter"
			}
			return "standby"
		}
	}
	return "non-member"
}
