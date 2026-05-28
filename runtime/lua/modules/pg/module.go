// SPDX-License-Identifier: MPL-2.0

// Package pg provides process groups operations for Lua.
package pg

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	rtresource "github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
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

// --- Instance: acquired PG scope resource (returned by pg.open) ---

const pgInstanceTypeName = "pg.Instance"

// Instance wraps an acquired PG scope resource. It holds a reference to the
// underlying ScopeService and the resource handle for lifecycle management.
// Created by pg.open() and released automatically on frame cleanup or manually
// via :release().
type Instance struct {
	resource      resource.Resource[any]
	svc           pgapi.ScopeService
	cancelCleanup func()
	id            string
	mu            sync.Mutex
	released      bool
}

// newPGInstance creates an Instance with automatic cleanup registration.
func newPGInstance(ctx context.Context, id string, res resource.Resource[any], svc pgapi.ScopeService) *Instance {
	inst := &Instance{
		id:       id,
		resource: res,
		svc:      svc,
		released: false,
	}

	resStore := rtresource.GetStore(ctx)
	if resStore != nil {
		inst.cancelCleanup = resStore.AddCleanup(func() error {
			inst.mu.Lock()
			defer inst.mu.Unlock()
			if !inst.released && inst.resource != nil {
				inst.resource.Release()
				inst.released = true
			}
			return nil
		})
	}

	return inst
}

var pgInstanceMethods = map[string]lua.LGoFunc{
	"join":               pgInstanceJoin,
	"leave":              pgInstanceLeave,
	"get_members":        pgInstanceGetMembers,
	"get_local_members":  pgInstanceGetLocalMembers,
	"which_groups":       pgInstanceWhichGroups,
	"which_local_groups": pgInstanceWhichLocalGroups,
	"broadcast":          pgInstanceBroadcast,
	"broadcast_local":    pgInstanceBroadcastLocal,
	"events":             pgInstanceEvents,
	"monitor":            pgInstanceMonitor,
	"release":            pgInstanceRelease,
}

func init() {
	value.RegisterTypeMethods(nil, pgInstanceTypeName,
		map[string]lua.LGoFunc{"__tostring": pgInstanceToString},
		pgInstanceMethods)
}

func checkPGInstance(l *lua.LState) *Instance {
	ud := l.CheckUserData(1)
	if inst, ok := ud.Value.(*Instance); ok {
		return inst
	}
	l.ArgError(1, "pg.Instance expected")
	return nil
}

func pgInstanceToString(l *lua.LState) int {
	inst := checkPGInstance(l)
	if inst == nil {
		return 0
	}
	inst.mu.Lock()
	released := inst.released
	inst.mu.Unlock()

	if released {
		l.Push(lua.LString(fmt.Sprintf("pg.Instance{%s, released}", inst.id)))
	} else {
		l.Push(lua.LString(fmt.Sprintf("pg.Instance{%s}", inst.id)))
	}
	return 1
}

func pgInstanceRelease(l *lua.LState) int {
	inst := checkPGInstance(l)
	if inst == nil {
		return 0
	}

	inst.mu.Lock()
	if !inst.released && inst.resource != nil {
		inst.resource.Release()
		inst.resource = nil
		inst.released = true
		cancel := inst.cancelCleanup
		inst.cancelCleanup = nil
		inst.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	} else {
		inst.mu.Unlock()
	}

	l.Push(lua.LTrue)
	return 1
}

// checkInstanceReady validates the instance is alive and returns the ScopeService.
func checkInstanceReady(l *lua.LState, inst *Instance) (pgapi.ScopeService, bool) {
	inst.mu.Lock()
	released := inst.released
	svc := inst.svc
	inst.mu.Unlock()
	if released {
		pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "pg instance is released"))
		return nil, false
	}
	return svc, true
}

func pgInstanceJoin(l *lua.LState) int {
	inst := checkPGInstance(l)
	if inst == nil {
		return 0
	}
	svc, ok := checkInstanceReady(l, inst)
	if !ok {
		return 2
	}

	self, ok := checkPIDFromArg(l)
	if !ok {
		return 2
	}

	arg := l.Get(2)
	switch v := arg.(type) {
	case lua.LString:
		group := string(v)
		if group == "" {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
		}
		if !security.IsAllowed(l.Context(), "pg.join", group, nil) {
			return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to join group: %s", group)))
		}
		yield := AcquireJoinYield(svc, group, self)
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
		yield := AcquireJoinGroupsYield(svc, groups, self)
		l.Push(yield)
		return -1

	default:
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name (string) or groups table required"))
	}
}

func pgInstanceLeave(l *lua.LState) int {
	inst := checkPGInstance(l)
	if inst == nil {
		return 0
	}
	svc, ok := checkInstanceReady(l, inst)
	if !ok {
		return 2
	}

	self, ok := checkPIDFromArg(l)
	if !ok {
		return 2
	}

	arg := l.Get(2)
	switch v := arg.(type) {
	case lua.LString:
		group := string(v)
		if group == "" {
			return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
		}
		if !security.IsAllowed(l.Context(), "pg.leave", group, nil) {
			return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to leave group: %s", group)))
		}
		yield := AcquireLeaveYield(svc, group, self)
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
		yield := AcquireLeaveGroupsYield(svc, groups, self)
		l.Push(yield)
		return -1

	default:
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name (string) or groups table required"))
	}
}

func pgInstanceGetMembers(l *lua.LState) int {
	inst := checkPGInstance(l)
	if inst == nil {
		return 0
	}
	svc, ok := checkInstanceReady(l, inst)
	if !ok {
		return 2
	}

	group := l.CheckString(2)
	if group == "" {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
	}

	if !security.IsAllowed(l.Context(), "pg.get_members", group, nil) {
		return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to get members of group: %s", group)))
	}

	yield := AcquireGetMembersYield(svc, group)
	l.Push(yield)
	return -1
}

func pgInstanceGetLocalMembers(l *lua.LState) int {
	inst := checkPGInstance(l)
	if inst == nil {
		return 0
	}
	svc, ok := checkInstanceReady(l, inst)
	if !ok {
		return 2
	}

	group := l.CheckString(2)
	if group == "" {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
	}

	if !security.IsAllowed(l.Context(), "pg.get_local_members", group, nil) {
		return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to get local members of group: %s", group)))
	}

	yield := AcquireGetLocalMembersYield(svc, group)
	l.Push(yield)
	return -1
}

func pgInstanceWhichGroups(l *lua.LState) int {
	inst := checkPGInstance(l)
	if inst == nil {
		return 0
	}
	svc, ok := checkInstanceReady(l, inst)
	if !ok {
		return 2
	}

	if !security.IsAllowed(l.Context(), "pg.which_groups", "", nil) {
		return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, "not allowed to list groups"))
	}

	yield := AcquireWhichGroupsYield(svc)
	l.Push(yield)
	return -1
}

func pgInstanceWhichLocalGroups(l *lua.LState) int {
	inst := checkPGInstance(l)
	if inst == nil {
		return 0
	}
	svc, ok := checkInstanceReady(l, inst)
	if !ok {
		return 2
	}

	if !security.IsAllowed(l.Context(), "pg.which_local_groups", "", nil) {
		return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, "not allowed to list local groups"))
	}

	yield := AcquireWhichLocalGroupsYield(svc)
	l.Push(yield)
	return -1
}

func pgInstanceBroadcast(l *lua.LState) int {
	inst := checkPGInstance(l)
	if inst == nil {
		return 0
	}
	svc, ok := checkInstanceReady(l, inst)
	if !ok {
		return 2
	}

	self, ok := checkPIDFromArg(l)
	if !ok {
		return 2
	}

	group := l.CheckString(2)
	if group == "" {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
	}

	topic := l.CheckString(3)
	if topic == "" {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "topic is required"))
	}

	if !security.IsAllowed(l.Context(), "pg.broadcast", group, nil) {
		return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to broadcast to group: %s", group)))
	}

	var payloads payload.Payloads
	for i := 4; i <= l.GetTop(); i++ {
		payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
	}

	yield := AcquireBroadcastYield(svc, self, group, topic, payloads)
	l.Push(yield)
	return -1
}

func pgInstanceBroadcastLocal(l *lua.LState) int {
	inst := checkPGInstance(l)
	if inst == nil {
		return 0
	}
	svc, ok := checkInstanceReady(l, inst)
	if !ok {
		return 2
	}

	self, ok := checkPIDFromArg(l)
	if !ok {
		return 2
	}

	group := l.CheckString(2)
	if group == "" {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "group name is required"))
	}

	topic := l.CheckString(3)
	if topic == "" {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "topic is required"))
	}

	if !security.IsAllowed(l.Context(), "pg.broadcast_local", group, nil) {
		return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to broadcast locally to group: %s", group)))
	}

	var payloads payload.Payloads
	for i := 4; i <= l.GetTop(); i++ {
		payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
	}

	yield := AcquireBroadcastLocalYield(svc, self, group, topic, payloads)
	l.Push(yield)
	return -1
}

func pgInstanceEvents(l *lua.LState) int {
	inst := checkPGInstance(l)
	if inst == nil {
		return 0
	}
	svc, ok := checkInstanceReady(l, inst)
	if !ok {
		return 2
	}

	ctx := l.Context()
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

	yield := AcquireEventsYield(svc, ch, p, topic)
	l.Push(yield)
	return -1
}

func pgInstanceMonitor(l *lua.LState) int {
	inst := checkPGInstance(l)
	if inst == nil {
		return 0
	}
	svc, ok := checkInstanceReady(l, inst)
	if !ok {
		return 2
	}

	ctx := l.Context()

	group := l.CheckString(2)
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

	ch := engine.NewChannel(64)
	subID := atomic.AddUint64(&pgEventsCounter, 1)
	topic := fmt.Sprintf("pg.monitor@%d", subID)

	yield := AcquireMonitorYield(svc, ch, group, p, topic)
	l.Push(yield)
	return -1
}

// --- pg.open() resource acquisition ---

// pgOpen acquires a PG scope instance from the resource registry.
// Usage: local pg_scope = pg.open("app:pg")
// The returned instance supports all PG operations as methods (colon syntax).
func pgOpen(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, "no context found"))
	}

	id := l.CheckString(1)
	if id == "" {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, "resource id is required"))
	}

	if !security.IsAllowed(ctx, "pg.open", id, nil) {
		return pushPGError(l, lua.LNil, newPGError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to access pg scope: %s", id)))
	}

	reg := resource.GetRegistry(ctx)
	if reg == nil {
		return pushPGError(l, lua.LNil, newPGError(l, lua.NotFound, "resource registry not found"))
	}

	resID := registry.ParseID(id)
	res, err := reg.Acquire(ctx, resID, resource.ModeNormal)
	if err != nil {
		return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, fmt.Sprintf("failed to acquire pg scope: %v", err)))
	}

	raw, err := res.Get()
	if err != nil {
		res.Release()
		return pushPGError(l, lua.LNil, newPGError(l, lua.Internal, fmt.Sprintf("failed to get pg scope resource: %v", err)))
	}

	svc, ok := raw.(pgapi.ScopeService)
	if !ok {
		res.Release()
		return pushPGError(l, lua.LNil, newPGError(l, lua.Invalid, fmt.Sprintf("resource is not a pg scope: %T", raw)))
	}

	inst := newPGInstance(ctx, id, res, svc)
	value.PushTypedUserData(l, inst, pgInstanceTypeName)
	return 1
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 1)
	mod.RawSetString("open", lua.LGoFunc(pgOpen))
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

func pushPGError(l *lua.LState, v lua.LValue, err *lua.Error) int {
	l.Push(v)
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

// checkPIDFromArg gets the process PID from context (used by instance methods
// where arg 1 is self/userdata).
func checkPIDFromArg(l *lua.LState) (pid.PID, bool) {
	return checkPID(l)
}
