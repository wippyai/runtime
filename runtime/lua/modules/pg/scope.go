// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"fmt"
	"sync/atomic"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/security"
)

// ScopeSeparator is the delimiter between scope name and group name.
const ScopeSeparator = "::"

// scope creates a scoped pg module table. The returned table has the same
// functions as the pg module, but all group names are automatically prefixed
// with "scopeName::" to provide namespace isolation. This is analogous to
// named scopes in Erlang/OTP pg, implemented as group name prefixing.
func scope(l *lua.LState) int {
	scopeName := l.CheckString(1)
	if scopeName == "" {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "scope name is required"))
	}

	prefix := scopeName + ScopeSeparator

	mod := lua.CreateTable(0, 11)
	mod.RawSetString("name", lua.LString(scopeName))
	mod.RawSetString("join", scopedJoin(prefix))
	mod.RawSetString("leave", scopedLeave(prefix))
	mod.RawSetString("get_members", scopedGetMembers(prefix))
	mod.RawSetString("get_local_members", scopedGetLocalMembers(prefix))
	mod.RawSetString("which_groups", scopedWhichGroups(prefix))
	mod.RawSetString("which_local_groups", scopedWhichLocalGroups(prefix))
	mod.RawSetString("broadcast", scopedBroadcast(prefix))
	mod.RawSetString("broadcast_local", scopedBroadcastLocal(prefix))
	mod.RawSetString("events", scopedEvents(prefix))
	mod.RawSetString("monitor", scopedMonitor(prefix))
	mod.Immutable = true

	l.Push(mod)
	return 1
}

func scopedJoin(prefix string) lua.LGoFunc {
	return func(l *lua.LState) int {
		self, ok := checkPID(l)
		if !ok {
			return 2
		}

		arg := l.Get(1)
		switch v := arg.(type) {
		case lua.LString:
			group := string(v)
			if group == "" {
				return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
			}
			scopedGroup := prefix + group
			if !security.IsAllowed(l.Context(), "pg.join", scopedGroup, nil) {
				return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to join group: %s", scopedGroup)))
			}
			yield := AcquireJoinYield(scopedGroup, self)
			l.Push(yield)
			return -1

		case *lua.LTable:
			groups := make([]string, 0, v.Len())
			v.ForEach(func(_, val lua.LValue) {
				if s, ok := val.(lua.LString); ok {
					groups = append(groups, prefix+string(s))
				}
			})
			if len(groups) == 0 {
				return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "groups table must not be empty"))
			}
			for _, g := range groups {
				if !security.IsAllowed(l.Context(), "pg.join", g, nil) {
					return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to join group: %s", g)))
				}
			}
			yield := AcquireJoinGroupsYield(groups, self)
			l.Push(yield)
			return -1

		default:
			return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name (string) or groups table required"))
		}
	}
}

func scopedLeave(prefix string) lua.LGoFunc {
	return func(l *lua.LState) int {
		self, ok := checkPID(l)
		if !ok {
			return 2
		}

		arg := l.Get(1)
		switch v := arg.(type) {
		case lua.LString:
			group := string(v)
			if group == "" {
				return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
			}
			scopedGroup := prefix + group
			if !security.IsAllowed(l.Context(), "pg.leave", scopedGroup, nil) {
				return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to leave group: %s", scopedGroup)))
			}
			yield := AcquireLeaveYield(scopedGroup, self)
			l.Push(yield)
			return -1

		case *lua.LTable:
			groups := make([]string, 0, v.Len())
			v.ForEach(func(_, val lua.LValue) {
				if s, ok := val.(lua.LString); ok {
					groups = append(groups, prefix+string(s))
				}
			})
			if len(groups) == 0 {
				return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "groups table must not be empty"))
			}
			for _, g := range groups {
				if !security.IsAllowed(l.Context(), "pg.leave", g, nil) {
					return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to leave group: %s", g)))
				}
			}
			yield := AcquireLeaveGroupsYield(groups, self)
			l.Push(yield)
			return -1

		default:
			return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name (string) or groups table required"))
		}
	}
}

func scopedGetMembers(prefix string) lua.LGoFunc {
	return func(l *lua.LState) int {
		ctx := l.Context()
		if ctx == nil {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no context found"))
		}

		group := l.CheckString(1)
		if group == "" {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
		}
		scopedGroup := prefix + group

		if !security.IsAllowed(ctx, "pg.get_members", scopedGroup, nil) {
			return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to get members of group: %s", scopedGroup)))
		}

		yield := AcquireGetMembersYield(scopedGroup)
		l.Push(yield)
		return -1
	}
}

func scopedGetLocalMembers(prefix string) lua.LGoFunc {
	return func(l *lua.LState) int {
		ctx := l.Context()
		if ctx == nil {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no context found"))
		}

		group := l.CheckString(1)
		if group == "" {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
		}
		scopedGroup := prefix + group

		if !security.IsAllowed(ctx, "pg.get_local_members", scopedGroup, nil) {
			return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to get local members of group: %s", scopedGroup)))
		}

		yield := AcquireGetLocalMembersYield(scopedGroup)
		l.Push(yield)
		return -1
	}
}

func scopedWhichGroups(prefix string) lua.LGoFunc {
	return func(l *lua.LState) int {
		ctx := l.Context()
		if ctx == nil {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no context found"))
		}

		if !security.IsAllowed(ctx, "pg.which_groups", "", nil) {
			return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, "not allowed to list groups"))
		}

		yield := AcquireWhichGroupsYield()
		yield.Scope = prefix
		l.Push(yield)
		return -1
	}
}

func scopedWhichLocalGroups(prefix string) lua.LGoFunc {
	return func(l *lua.LState) int {
		ctx := l.Context()
		if ctx == nil {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no context found"))
		}

		if !security.IsAllowed(ctx, "pg.which_local_groups", "", nil) {
			return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, "not allowed to list local groups"))
		}

		yield := AcquireWhichLocalGroupsYield()
		yield.Scope = prefix
		l.Push(yield)
		return -1
	}
}

func scopedBroadcast(prefix string) lua.LGoFunc {
	return func(l *lua.LState) int {
		self, ok := checkPID(l)
		if !ok {
			return 2
		}

		group := l.CheckString(1)
		if group == "" {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
		}
		scopedGroup := prefix + group

		topic := l.CheckString(2)
		if topic == "" {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "topic is required"))
		}

		if !security.IsAllowed(l.Context(), "pg.broadcast", scopedGroup, nil) {
			return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to broadcast to group: %s", scopedGroup)))
		}

		var payloads payload.Payloads
		for i := 3; i <= l.GetTop(); i++ {
			payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
		}

		yield := AcquireBroadcastYield(self, scopedGroup, topic, payloads)
		l.Push(yield)
		return -1
	}
}

func scopedBroadcastLocal(prefix string) lua.LGoFunc {
	return func(l *lua.LState) int {
		self, ok := checkPID(l)
		if !ok {
			return 2
		}

		group := l.CheckString(1)
		if group == "" {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
		}
		scopedGroup := prefix + group

		topic := l.CheckString(2)
		if topic == "" {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "topic is required"))
		}

		if !security.IsAllowed(l.Context(), "pg.broadcast_local", scopedGroup, nil) {
			return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to broadcast locally to group: %s", scopedGroup)))
		}

		var payloads payload.Payloads
		for i := 3; i <= l.GetTop(); i++ {
			payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
		}

		yield := AcquireBroadcastLocalYield(self, scopedGroup, topic, payloads)
		l.Push(yield)
		return -1
	}
}

func scopedEvents(prefix string) lua.LGoFunc {
	return func(l *lua.LState) int {
		ctx := l.Context()
		if ctx == nil {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no context found"))
		}

		if !security.IsAllowed(ctx, "pg.events", "", nil) {
			return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, "not allowed to subscribe to pg events"))
		}

		p, ok := runtime.GetFramePID(ctx)
		if !ok {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no process PID"))
		}

		ch := engine.NewChannel(64)
		subID := atomic.AddUint64(&pgEventsCounter, 1)
		topic := fmt.Sprintf("pg.events@%d", subID)

		yield := AcquireEventsYield(ch, p, topic)
		yield.Scope = prefix
		l.Push(yield)
		return -1
	}
}

func scopedMonitor(prefix string) lua.LGoFunc {
	return func(l *lua.LState) int {
		ctx := l.Context()
		if ctx == nil {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no context found"))
		}

		group := l.CheckString(1)
		if group == "" {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
		}
		scopedGroup := prefix + group

		if !security.IsAllowed(ctx, "pg.monitor", scopedGroup, nil) {
			return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to monitor group: %s", scopedGroup)))
		}

		p, ok := runtime.GetFramePID(ctx)
		if !ok {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no process PID"))
		}

		ch := engine.NewChannel(64)
		subID := atomic.AddUint64(&pgEventsCounter, 1)
		topic := fmt.Sprintf("pg.monitor@%d", subID)

		yield := AcquireMonitorYield(ch, scopedGroup, p, topic)
		l.Push(yield)
		return -1
	}
}
