// SPDX-License-Identifier: MPL-2.0

// Package pg provides process groups operations for Lua.
package pg

import (
	"fmt"
	"sync/atomic"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/security"
)

// Module is the pg module definition.
var Module = &luaapi.ModuleDef{
	Name:        "pg",
	Description: "Distributed named process groups",
	Class:       []string{luaapi.ClassProcess, luaapi.ClassNondeterministic},
	Build:       buildModule,
	Types:       ModuleTypes,
}

var pgEventsCounter uint64

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 11)
	mod.RawSetString("join", lua.LGoFunc(join))
	mod.RawSetString("leave", lua.LGoFunc(leave))
	mod.RawSetString("get_members", lua.LGoFunc(getMembers))
	mod.RawSetString("get_local_members", lua.LGoFunc(getLocalMembers))
	mod.RawSetString("which_groups", lua.LGoFunc(whichGroups))
	mod.RawSetString("which_local_groups", lua.LGoFunc(whichLocalGroups))
	mod.RawSetString("broadcast", lua.LGoFunc(broadcast))
	mod.RawSetString("broadcast_local", lua.LGoFunc(broadcastLocal))
	mod.RawSetString("events", lua.LGoFunc(events))
	mod.RawSetString("monitor", lua.LGoFunc(monitor))
	mod.RawSetString("scope", lua.LGoFunc(scope))
	mod.Immutable = true

	yields := []luaapi.YieldType{
		{Sample: &JoinYield{}, CmdID: pgapi.Join},
		{Sample: &JoinGroupsYield{}, CmdID: pgapi.JoinGroups},
		{Sample: &LeaveYield{}, CmdID: pgapi.Leave},
		{Sample: &LeaveGroupsYield{}, CmdID: pgapi.LeaveGroups},
		{Sample: &GetMembersYield{}, CmdID: pgapi.GetMembers},
		{Sample: &GetLocalMembersYield{}, CmdID: pgapi.GetLocalMembers},
		{Sample: &WhichGroupsYield{}, CmdID: pgapi.WhichGroups},
		{Sample: &WhichLocalGroupsYield{}, CmdID: pgapi.WhichLocalGroups},
		{Sample: &BroadcastYield{}, CmdID: pgapi.Broadcast},
		{Sample: &BroadcastLocalYield{}, CmdID: pgapi.BroadcastLocal},
		{Sample: &EventsYield{}, CmdID: pgapi.Events},
		{Sample: &MonitorYield{}, CmdID: pgapi.Monitor},
	}

	return mod, yields
}

func newPGError(l *lua.LState, kind lua.Kind, message string) *lua.Error {
	return lua.NewLuaError(l, message).
		WithKind(kind).
		WithRetryable(false)
}

func pushPGError(l *lua.LState, value lua.LValue, err *lua.Error) int {
	l.Push(value)
	l.Push(err)
	return 2
}

func checkPID(l *lua.LState) (pid.PID, bool) {
	ctx := l.Context()
	if ctx == nil {
		pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no context found"))
		return pid.PID{}, false
	}

	p, ok := runtime.GetFramePID(ctx)
	if !ok {
		pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no process PID"))
		return pid.PID{}, false
	}
	return p, true
}

func join(l *lua.LState) int {
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
		if !security.IsAllowed(l.Context(), "pg.join", group, nil) {
			return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to join group: %s", group)))
		}
		yield := AcquireJoinYield(group, self)
		l.Push(yield)
		return -1

	case *lua.LTable:
		groups := make([]string, 0, v.Len())
		v.ForEach(func(_, val lua.LValue) {
			if s, ok := val.(lua.LString); ok {
				groups = append(groups, string(s))
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

func leave(l *lua.LState) int {
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
		if !security.IsAllowed(l.Context(), "pg.leave", group, nil) {
			return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to leave group: %s", group)))
		}
		yield := AcquireLeaveYield(group, self)
		l.Push(yield)
		return -1

	case *lua.LTable:
		groups := make([]string, 0, v.Len())
		v.ForEach(func(_, val lua.LValue) {
			if s, ok := val.(lua.LString); ok {
				groups = append(groups, string(s))
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

func getMembers(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no context found"))
	}

	group := l.CheckString(1)
	if group == "" {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
	}

	if !security.IsAllowed(ctx, "pg.get_members", group, nil) {
		return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to get members of group: %s", group)))
	}

	yield := AcquireGetMembersYield(group)
	l.Push(yield)
	return -1
}

func getLocalMembers(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no context found"))
	}

	group := l.CheckString(1)
	if group == "" {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
	}

	if !security.IsAllowed(ctx, "pg.get_local_members", group, nil) {
		return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to get local members of group: %s", group)))
	}

	yield := AcquireGetLocalMembersYield(group)
	l.Push(yield)
	return -1
}

func whichGroups(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no context found"))
	}

	if !security.IsAllowed(ctx, "pg.which_groups", "", nil) {
		return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, "not allowed to list groups"))
	}

	yield := AcquireWhichGroupsYield()
	l.Push(yield)
	return -1
}

func whichLocalGroups(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no context found"))
	}

	if !security.IsAllowed(ctx, "pg.which_local_groups", "", nil) {
		return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, "not allowed to list local groups"))
	}

	yield := AcquireWhichLocalGroupsYield()
	l.Push(yield)
	return -1
}

func broadcast(l *lua.LState) int {
	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	group := l.CheckString(1)
	if group == "" {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
	}

	topic := l.CheckString(2)
	if topic == "" {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "topic is required"))
	}

	if !security.IsAllowed(l.Context(), "pg.broadcast", group, nil) {
		return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to broadcast to group: %s", group)))
	}

	// Collect payload arguments (starting from arg 3)
	var payloads payload.Payloads
	for i := 3; i <= l.GetTop(); i++ {
		payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
	}

	yield := AcquireBroadcastYield(self, group, topic, payloads)
	l.Push(yield)
	return -1
}

func broadcastLocal(l *lua.LState) int {
	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	group := l.CheckString(1)
	if group == "" {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
	}

	topic := l.CheckString(2)
	if topic == "" {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "topic is required"))
	}

	if !security.IsAllowed(l.Context(), "pg.broadcast_local", group, nil) {
		return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to broadcast locally to group: %s", group)))
	}

	// Collect payload arguments (starting from arg 3)
	var payloads payload.Payloads
	for i := 3; i <= l.GetTop(); i++ {
		payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
	}

	yield := AcquireBroadcastLocalYield(self, group, topic, payloads)
	l.Push(yield)
	return -1
}

func events(l *lua.LState) int {
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

	// Create channel and unique topic for this subscription
	ch := engine.NewChannel(64)
	subID := atomic.AddUint64(&pgEventsCounter, 1)
	topic := fmt.Sprintf("pg.events@%d", subID)

	yield := AcquireEventsYield(ch, p, topic)
	l.Push(yield)
	return -1
}

func monitor(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no context found"))
	}

	group := l.CheckString(1)
	if group == "" {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
	}

	if !security.IsAllowed(ctx, "pg.monitor", group, nil) {
		return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to monitor group: %s", group)))
	}

	p, ok := runtime.GetFramePID(ctx)
	if !ok {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no process PID"))
	}

	// Create channel and unique topic for this monitor subscription
	ch := engine.NewChannel(64)
	subID := atomic.AddUint64(&pgEventsCounter, 1)
	topic := fmt.Sprintf("pg.monitor@%d", subID)

	yield := AcquireMonitorYield(ch, group, p, topic)
	l.Push(yield)
	return -1
}
