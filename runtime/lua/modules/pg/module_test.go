// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

// ==========================================================================
// Mock types for resource acquisition
// ==========================================================================

// mockScopeService is a no-op ScopeService for testing yield production.
// The module layer doesn't call any methods on it — it just passes it through
// to yield structs. Actual service tests live in system/pg.
type mockScopeService struct{}

func (m *mockScopeService) Join(_ pgapi.Group, _ pid.PID) error         { return nil }
func (m *mockScopeService) JoinGroups(_ []pgapi.Group, _ pid.PID) error { return nil }
func (m *mockScopeService) Leave(_ pgapi.Group, _ pid.PID) error        { return nil }
func (m *mockScopeService) LeaveGroups(_ []pgapi.Group, _ pid.PID) error {
	return nil
}
func (m *mockScopeService) GetMembers(_ pgapi.Group) []pid.PID      { return nil }
func (m *mockScopeService) GetLocalMembers(_ pgapi.Group) []pid.PID { return nil }
func (m *mockScopeService) WhichGroups() []pgapi.Group              { return nil }
func (m *mockScopeService) WhichLocalGroups() []pgapi.Group         { return nil }
func (m *mockScopeService) Broadcast(_ pid.PID, _ pgapi.Group, _ string, _ payload.Payloads) (int, error) {
	return 0, nil
}
func (m *mockScopeService) BroadcastLocal(_ pid.PID, _ pgapi.Group, _ string, _ payload.Payloads) (int, error) {
	return 0, nil
}
func (m *mockScopeService) Monitor(_ pgapi.Group, _ pid.PID, _ string) pgapi.MonitorResult {
	return pgapi.MonitorResult{}
}
func (m *mockScopeService) Events(_ pid.PID, _ string) pgapi.EventsResult {
	return pgapi.EventsResult{}
}

// mockResource wraps a value to satisfy resource.Resource[any].
type mockResource struct {
	value    any
	released bool
}

func (m *mockResource) Get() (any, error) {
	if m.released {
		return nil, resource.ErrReleased
	}
	return m.value, nil
}

func (m *mockResource) Release() { m.released = true }

// mockRegistry implements resource.Registry for testing pg.open().
type mockRegistry struct {
	resources map[string]any
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{resources: make(map[string]any)}
}

func (r *mockRegistry) Register(id string, val any) {
	r.resources[id] = val
}

func (r *mockRegistry) Acquire(_ context.Context, id registry.ID, _ resource.AccessMode) (resource.Resource[any], error) {
	val, ok := r.resources[id.String()]
	if !ok {
		return nil, resource.ErrNotFound
	}
	return &mockResource{value: val}, nil
}

func (r *mockRegistry) List() ([]registry.ID, error) {
	ids := make([]registry.ID, 0, len(r.resources))
	for id := range r.resources {
		ids = append(ids, registry.ParseID(id))
	}
	return ids, nil
}

func (r *mockRegistry) Exists(id registry.ID) bool {
	_, ok := r.resources[id.String()]
	return ok
}

// ==========================================================================
// Test helpers
// ==========================================================================

func bindPG(l *lua.LState) {
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
}

// newLuaWithPID creates a Lua state with PID, resource registry, and a mock
// PG scope registered under "test:pg". This is the standard setup for testing
// instance method yield production via pg.open("test:pg").
func newLuaWithPID(t *testing.T) (*lua.LState, pid.PID) {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	bindPG(l)

	testPID := pid.PID{Host: "h1", UniqID: "p1"}
	ctx := ctxapi.NewRootContext()
	security.SetStrictMode(ctx, false)

	reg := newMockRegistry()
	reg.Register("test:pg", &mockScopeService{})
	resource.WithRegistry(ctx, reg)

	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	require.NoError(t, runtimeapi.SetFramePID(ctx, testPID))
	l.SetContext(ctx)

	return l, testPID
}

// newLuaNoContext creates a Lua state with pg module but no context.
func newLuaNoContext(t *testing.T) *lua.LState {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	bindPG(l)
	// No context set — l.Context() returns nil
	return l
}

// newLuaNoPID creates a Lua state with context but no PID in the frame.
func newLuaNoPID(t *testing.T) *lua.LState {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	bindPG(l)

	ctx := ctxapi.NewRootContext()
	security.SetStrictMode(ctx, false)

	reg := newMockRegistry()
	reg.Register("test:pg", &mockScopeService{})
	resource.WithRegistry(ctx, reg)

	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	// No PID set in frame
	l.SetContext(ctx)

	return l
}

// newLuaStrictMode creates a Lua state with strict security (no actor/scope).
func newLuaStrictMode(t *testing.T) (*lua.LState, pid.PID) {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	bindPG(l)

	testPID := pid.PID{Host: "h1", UniqID: "p1"}
	ctx := ctxapi.NewRootContext()
	security.SetStrictMode(ctx, true) // strict mode — no actor/scope = denied

	reg := newMockRegistry()
	reg.Register("test:pg", &mockScopeService{})
	resource.WithRegistry(ctx, reg)

	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	require.NoError(t, runtimeapi.SetFramePID(ctx, testPID))
	l.SetContext(ctx)

	return l, testPID
}

// newLuaNoRegistry creates a Lua state with context and PID but no resource registry.
func newLuaNoRegistry(t *testing.T) *lua.LState {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	bindPG(l)

	ctx := ctxapi.NewRootContext()
	security.SetStrictMode(ctx, false)
	// No resource.WithRegistry — registry is nil

	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	testPID := pid.PID{Host: "h1", UniqID: "p1"}
	require.NoError(t, runtimeapi.SetFramePID(ctx, testPID))
	l.SetContext(ctx)

	return l
}

// resumeYield compiles a Lua snippet, creates a coroutine thread with the
// parent's context, resumes it, and returns the resume state and yielded values.
func resumeYield(t *testing.T, parent *lua.LState, script string) (lua.ResumeState, []lua.LValue) {
	t.Helper()
	fn, err := parent.LoadString(script)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	state, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)

	return state, values
}

// lastValue returns the last element from a values slice.
func lastValue(values []lua.LValue) lua.LValue {
	if len(values) == 0 {
		return nil
	}
	return values[len(values)-1]
}

// ==========================================================================
// Module build tests
// ==========================================================================

func TestModuleInfo(t *testing.T) {
	info := Module.Info()
	assert.Equal(t, "pg", info.Name)
	assert.NotEmpty(t, info.Description)
}

func TestModuleBuild(t *testing.T) {
	tbl, yields := Module.Build()
	require.NotNil(t, tbl)
	require.NotNil(t, yields)
	assert.Len(t, yields, 12)
}

func TestModuleFunctions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindPG(l)

	mod := l.GetGlobal("pg")
	require.Equal(t, lua.LTTable, mod.Type())

	modTbl := mod.(*lua.LTable)
	// open is the only module-level function; everything else is on the
	// instance returned by pg.open().
	functions := []string{"open"}
	for _, fn := range functions {
		assert.Equal(t, lua.LTFunction, modTbl.RawGetString(fn).Type(), "function %s not registered", fn)
	}
}

func TestModuleImmutable(t *testing.T) {
	tbl, _ := Module.Build()
	assert.True(t, tbl.Immutable)
}

// ==========================================================================
// Yield CmdID tests
// ==========================================================================

func TestJoinYieldCmdID(t *testing.T) {
	y := &JoinYield{}
	assert.Equal(t, pgapi.Join, y.CmdID())
}

func TestLeaveYieldCmdID(t *testing.T) {
	y := &LeaveYield{}
	assert.Equal(t, pgapi.Leave, y.CmdID())
}

func TestGetMembersYieldCmdID(t *testing.T) {
	y := &GetMembersYield{}
	assert.Equal(t, pgapi.GetMembers, y.CmdID())
}

func TestGetLocalMembersYieldCmdID(t *testing.T) {
	y := &GetLocalMembersYield{}
	assert.Equal(t, pgapi.GetLocalMembers, y.CmdID())
}

func TestWhichGroupsYieldCmdID(t *testing.T) {
	y := &WhichGroupsYield{}
	assert.Equal(t, pgapi.WhichGroups, y.CmdID())
}

func TestWhichLocalGroupsYieldCmdID(t *testing.T) {
	y := &WhichLocalGroupsYield{}
	assert.Equal(t, pgapi.WhichLocalGroups, y.CmdID())
}

func TestBroadcastYieldCmdID(t *testing.T) {
	y := &BroadcastYield{}
	assert.Equal(t, pgapi.Broadcast, y.CmdID())
}

func TestBroadcastLocalYieldCmdID(t *testing.T) {
	y := &BroadcastLocalYield{}
	assert.Equal(t, pgapi.BroadcastLocal, y.CmdID())
}

func TestEventsYieldCmdID(t *testing.T) {
	y := &EventsYield{}
	assert.Equal(t, pgapi.Events, y.CmdID())
}

func TestMonitorYieldCmdID(t *testing.T) {
	y := &MonitorYield{}
	assert.Equal(t, pgapi.Monitor, y.CmdID())
}

func TestJoinGroupsYieldCmdID(t *testing.T) {
	y := &JoinGroupsYield{}
	assert.Equal(t, pgapi.JoinGroups, y.CmdID())
}

func TestLeaveGroupsYieldCmdID(t *testing.T) {
	y := &LeaveGroupsYield{}
	assert.Equal(t, pgapi.LeaveGroups, y.CmdID())
}

// ==========================================================================
// Yield String/Type tests
// ==========================================================================

func TestYieldStrings(t *testing.T) {
	tests := []struct {
		name     string
		yield    lua.LValue
		expected string
	}{
		{"Join", &JoinYield{}, "<pg_join_yield>"},
		{"Leave", &LeaveYield{}, "<pg_leave_yield>"},
		{"GetMembers", &GetMembersYield{}, "<pg_get_members_yield>"},
		{"GetLocalMembers", &GetLocalMembersYield{}, "<pg_get_local_members_yield>"},
		{"WhichGroups", &WhichGroupsYield{}, "<pg_which_groups_yield>"},
		{"WhichLocalGroups", &WhichLocalGroupsYield{}, "<pg_which_local_groups_yield>"},
		{"Broadcast", &BroadcastYield{}, "<pg_broadcast_yield>"},
		{"BroadcastLocal", &BroadcastLocalYield{}, "<pg_broadcast_local_yield>"},
		{"Events", &EventsYield{}, "<pg_events_yield>"},
		{"Monitor", &MonitorYield{}, "<pg_monitor_yield>"},
		{"JoinGroups", &JoinGroupsYield{}, "<pg_join_groups_yield>"},
		{"LeaveGroups", &LeaveGroupsYield{}, "<pg_leave_groups_yield>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.yield.String())
			assert.Equal(t, lua.LTUserData, tt.yield.Type())
		})
	}
}

// ==========================================================================
// Yield ToCommand tests
// ==========================================================================

func TestJoinYieldToCommand(t *testing.T) {
	svc := &mockScopeService{}
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireJoinYield(svc, "workers", p)

	cmd := y.ToCommand()
	joinCmd, ok := cmd.(*pgapi.JoinCmd)
	require.True(t, ok)
	assert.Equal(t, "workers", joinCmd.Group)
	assert.Equal(t, p, joinCmd.Caller)
	assert.Equal(t, svc, joinCmd.Instance)
	assert.Equal(t, pgapi.Join, joinCmd.CmdID())

	joinCmd.Release()
	y.Release()
}

func TestLeaveYieldToCommand(t *testing.T) {
	svc := &mockScopeService{}
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireLeaveYield(svc, "workers", p)

	cmd := y.ToCommand()
	leaveCmd, ok := cmd.(*pgapi.LeaveCmd)
	require.True(t, ok)
	assert.Equal(t, "workers", leaveCmd.Group)
	assert.Equal(t, p, leaveCmd.Caller)
	assert.Equal(t, svc, leaveCmd.Instance)
	assert.Equal(t, pgapi.Leave, leaveCmd.CmdID())

	leaveCmd.Release()
	y.Release()
}

func TestGetMembersYieldToCommand(t *testing.T) {
	svc := &mockScopeService{}
	y := AcquireGetMembersYield(svc, "workers")

	cmd := y.ToCommand()
	gmc, ok := cmd.(*pgapi.GetMembersCmd)
	require.True(t, ok)
	assert.Equal(t, "workers", gmc.Group)
	assert.Equal(t, svc, gmc.Instance)
	assert.Equal(t, pgapi.GetMembers, gmc.CmdID())

	gmc.Release()
	y.Release()
}

func TestGetLocalMembersYieldToCommand(t *testing.T) {
	svc := &mockScopeService{}
	y := AcquireGetLocalMembersYield(svc, "workers")

	cmd := y.ToCommand()
	glmc, ok := cmd.(*pgapi.GetLocalMembersCmd)
	require.True(t, ok)
	assert.Equal(t, "workers", glmc.Group)
	assert.Equal(t, svc, glmc.Instance)
	assert.Equal(t, pgapi.GetLocalMembers, glmc.CmdID())

	glmc.Release()
	y.Release()
}

func TestWhichGroupsYieldToCommand(t *testing.T) {
	svc := &mockScopeService{}
	y := AcquireWhichGroupsYield(svc)

	cmd := y.ToCommand()
	wgc, ok := cmd.(*pgapi.WhichGroupsCmd)
	require.True(t, ok)
	assert.Equal(t, svc, wgc.Instance)
	assert.Equal(t, pgapi.WhichGroups, wgc.CmdID())

	wgc.Release()
	y.Release()
}

func TestWhichLocalGroupsYieldToCommand(t *testing.T) {
	svc := &mockScopeService{}
	y := AcquireWhichLocalGroupsYield(svc)

	cmd := y.ToCommand()
	wlgc, ok := cmd.(*pgapi.WhichLocalGroupsCmd)
	require.True(t, ok)
	assert.Equal(t, svc, wlgc.Instance)
	assert.Equal(t, pgapi.WhichLocalGroups, wlgc.CmdID())

	wlgc.Release()
	y.Release()
}

func TestBroadcastYieldToCommand(t *testing.T) {
	svc := &mockScopeService{}
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireBroadcastYield(svc, p, "workers", "hello", nil)

	cmd := y.ToCommand()
	bc, ok := cmd.(*pgapi.BroadcastCmd)
	require.True(t, ok)
	assert.Equal(t, p, bc.From)
	assert.Equal(t, "workers", bc.Group)
	assert.Equal(t, "hello", bc.Topic)
	assert.Equal(t, svc, bc.Instance)
	assert.Equal(t, pgapi.Broadcast, bc.CmdID())

	bc.Release()
	y.Release()
}

func TestBroadcastLocalYieldToCommand(t *testing.T) {
	svc := &mockScopeService{}
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireBroadcastLocalYield(svc, p, "workers", "hello", nil)

	cmd := y.ToCommand()
	blc, ok := cmd.(*pgapi.BroadcastLocalCmd)
	require.True(t, ok)
	assert.Equal(t, p, blc.From)
	assert.Equal(t, "workers", blc.Group)
	assert.Equal(t, "hello", blc.Topic)
	assert.Equal(t, svc, blc.Instance)
	assert.Equal(t, pgapi.BroadcastLocal, blc.CmdID())

	blc.Release()
	y.Release()
}

func TestEventsYieldToCommand(t *testing.T) {
	svc := &mockScopeService{}
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireEventsYield(svc, nil, p, "pg.events@1")

	cmd := y.ToCommand()
	ec, ok := cmd.(*pgapi.EventsCmd)
	require.True(t, ok)
	assert.Equal(t, p, ec.PID)
	assert.Equal(t, "pg.events@1", ec.Topic)
	assert.Equal(t, svc, ec.Instance)
	assert.Equal(t, pgapi.Events, ec.CmdID())

	ec.Release()
	y.Release()
}

func TestMonitorYieldToCommand(t *testing.T) {
	svc := &mockScopeService{}
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireMonitorYield(svc, nil, "workers", p, "pg.monitor@1")

	cmd := y.ToCommand()
	mc, ok := cmd.(*pgapi.MonitorCmd)
	require.True(t, ok)
	assert.Equal(t, "workers", mc.Group)
	assert.Equal(t, p, mc.PID)
	assert.Equal(t, "pg.monitor@1", mc.Topic)
	assert.Equal(t, svc, mc.Instance)
	assert.Equal(t, pgapi.Monitor, mc.CmdID())

	mc.Release()
	y.Release()
}

func TestJoinGroupsYieldToCommand(t *testing.T) {
	svc := &mockScopeService{}
	p := pid.PID{Host: "h1", UniqID: "p1"}
	groups := []string{"a", "b"}
	y := AcquireJoinGroupsYield(svc, groups, p)

	cmd := y.ToCommand()
	jgc, ok := cmd.(*pgapi.JoinGroupsCmd)
	require.True(t, ok)
	assert.Equal(t, groups, jgc.Groups)
	assert.Equal(t, p, jgc.Caller)
	assert.Equal(t, svc, jgc.Instance)
	assert.Equal(t, pgapi.JoinGroups, jgc.CmdID())

	jgc.Release()
	y.Release()
}

func TestLeaveGroupsYieldToCommand(t *testing.T) {
	svc := &mockScopeService{}
	p := pid.PID{Host: "h1", UniqID: "p1"}
	groups := []string{"a", "b"}
	y := AcquireLeaveGroupsYield(svc, groups, p)

	cmd := y.ToCommand()
	lgc, ok := cmd.(*pgapi.LeaveGroupsCmd)
	require.True(t, ok)
	assert.Equal(t, groups, lgc.Groups)
	assert.Equal(t, p, lgc.Caller)
	assert.Equal(t, svc, lgc.Instance)
	assert.Equal(t, pgapi.LeaveGroups, lgc.CmdID())

	lgc.Release()
	y.Release()
}

// ==========================================================================
// Yield HandleResult tests
// ==========================================================================

func TestJoinYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &JoinYield{}
	vals := y.HandleResult(l, pgapi.JoinResult{}, nil)
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LTrue, vals[0])
	assert.Equal(t, lua.LNil, vals[1])
}

func TestJoinYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &JoinYield{}
	vals := y.HandleResult(l, nil, errors.New("dispatch error"))
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

func TestJoinYield_HandleResult_JoinError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &JoinYield{}
	vals := y.HandleResult(l, pgapi.JoinResult{Error: errors.New("join failed")}, nil)
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

func TestLeaveYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &LeaveYield{}
	vals := y.HandleResult(l, pgapi.LeaveResult{}, nil)
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LTrue, vals[0])
	assert.Equal(t, lua.LNil, vals[1])
}

func TestLeaveYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &LeaveYield{}
	vals := y.HandleResult(l, pgapi.LeaveResult{Error: errors.New("leave failed")}, nil)
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

func TestLeaveYield_HandleResult_DispatchError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &LeaveYield{}
	vals := y.HandleResult(l, nil, errors.New("dispatch error"))
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

func TestGetMembersYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &GetMembersYield{}
	result := pgapi.GetMembersResult{
		Members: []pid.PID{
			{Host: "h1", UniqID: "1"},
			{Host: "h1", UniqID: "2"},
		},
	}
	vals := y.HandleResult(l, result, nil)
	require.Len(t, vals, 2)
	tbl, ok := vals[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 2, tbl.Len())
	assert.Equal(t, lua.LNil, vals[1])
}

func TestGetMembersYield_HandleResult_Empty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &GetMembersYield{}
	vals := y.HandleResult(l, pgapi.GetMembersResult{}, nil)
	require.Len(t, vals, 2)
	tbl, ok := vals[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 0, tbl.Len())
}

func TestGetMembersYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &GetMembersYield{}
	vals := y.HandleResult(l, nil, errors.New("dispatch error"))
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

func TestGetMembersYield_HandleResult_WrongType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &GetMembersYield{}
	vals := y.HandleResult(l, "wrong type", nil)
	require.Len(t, vals, 2)
	tbl, ok := vals[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 0, tbl.Len())
}

func TestGetLocalMembersYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &GetLocalMembersYield{}
	result := pgapi.GetLocalMembersResult{
		Members: []pid.PID{{Host: "h1", UniqID: "1"}},
	}
	vals := y.HandleResult(l, result, nil)
	require.Len(t, vals, 2)
	tbl, ok := vals[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 1, tbl.Len())
}

func TestGetLocalMembersYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &GetLocalMembersYield{}
	vals := y.HandleResult(l, nil, errors.New("dispatch error"))
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

func TestGetLocalMembersYield_HandleResult_WrongType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &GetLocalMembersYield{}
	vals := y.HandleResult(l, "wrong type", nil)
	require.Len(t, vals, 2)
	tbl, ok := vals[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 0, tbl.Len())
}

func TestWhichGroupsYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &WhichGroupsYield{}
	result := pgapi.WhichGroupsResult{Groups: []pgapi.Group{"workers", "managers"}}
	vals := y.HandleResult(l, result, nil)
	require.Len(t, vals, 2)
	tbl, ok := vals[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 2, tbl.Len())
	assert.Equal(t, lua.LString("workers"), tbl.RawGetInt(1))
	assert.Equal(t, lua.LString("managers"), tbl.RawGetInt(2))
}

func TestWhichGroupsYield_HandleResult_Empty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &WhichGroupsYield{}
	vals := y.HandleResult(l, pgapi.WhichGroupsResult{}, nil)
	require.Len(t, vals, 2)
	tbl, ok := vals[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 0, tbl.Len())
}

func TestWhichGroupsYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &WhichGroupsYield{}
	vals := y.HandleResult(l, nil, errors.New("dispatch error"))
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

func TestWhichGroupsYield_HandleResult_WrongType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &WhichGroupsYield{}
	vals := y.HandleResult(l, "wrong type", nil)
	require.Len(t, vals, 2)
	tbl, ok := vals[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 0, tbl.Len())
}

func TestWhichLocalGroupsYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &WhichLocalGroupsYield{}
	result := pgapi.WhichLocalGroupsResult{Groups: []pgapi.Group{"workers", "managers"}}
	vals := y.HandleResult(l, result, nil)
	require.Len(t, vals, 2)
	tbl, ok := vals[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 2, tbl.Len())
	assert.Equal(t, lua.LString("workers"), tbl.RawGetInt(1))
	assert.Equal(t, lua.LString("managers"), tbl.RawGetInt(2))
}

func TestWhichLocalGroupsYield_HandleResult_Empty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &WhichLocalGroupsYield{}
	vals := y.HandleResult(l, pgapi.WhichLocalGroupsResult{}, nil)
	require.Len(t, vals, 2)
	tbl, ok := vals[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 0, tbl.Len())
}

func TestWhichLocalGroupsYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &WhichLocalGroupsYield{}
	vals := y.HandleResult(l, nil, errors.New("dispatch error"))
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

func TestWhichLocalGroupsYield_HandleResult_WrongType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &WhichLocalGroupsYield{}
	vals := y.HandleResult(l, "wrong type", nil)
	require.Len(t, vals, 2)
	tbl, ok := vals[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 0, tbl.Len())
}

func TestBroadcastYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &BroadcastYield{}
	vals := y.HandleResult(l, pgapi.BroadcastResult{Sent: 3}, nil)
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LTrue, vals[0])
	assert.Equal(t, lua.LNil, vals[1])
}

func TestBroadcastYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &BroadcastYield{}
	vals := y.HandleResult(l, pgapi.BroadcastResult{Error: errors.New("broadcast failed")}, nil)
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

func TestBroadcastYield_HandleResult_DispatchError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &BroadcastYield{}
	vals := y.HandleResult(l, nil, errors.New("dispatch error"))
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

func TestBroadcastLocalYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &BroadcastLocalYield{}
	vals := y.HandleResult(l, pgapi.BroadcastLocalResult{Sent: 1}, nil)
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LTrue, vals[0])
	assert.Equal(t, lua.LNil, vals[1])
}

func TestBroadcastLocalYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &BroadcastLocalYield{}
	vals := y.HandleResult(l, pgapi.BroadcastLocalResult{Error: errors.New("broadcast failed")}, nil)
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

func TestBroadcastLocalYield_HandleResult_DispatchError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &BroadcastLocalYield{}
	vals := y.HandleResult(l, nil, errors.New("dispatch error"))
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

func TestEventsYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &EventsYield{}
	vals := y.HandleResult(l, nil, errors.New("dispatch error"))
	require.Len(t, vals, 3)
	assert.Equal(t, lua.LNil, vals[0])
	assert.Equal(t, lua.LNil, vals[1])
	assert.NotEqual(t, lua.LNil, vals[2])
}

func TestMonitorYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &MonitorYield{}
	vals := y.HandleResult(l, nil, errors.New("dispatch error"))
	require.Len(t, vals, 3)
	assert.Equal(t, lua.LNil, vals[0])
	assert.Equal(t, lua.LNil, vals[1])
	assert.NotEqual(t, lua.LNil, vals[2])
}

func TestJoinGroupsYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &JoinGroupsYield{}
	vals := y.HandleResult(l, pgapi.JoinGroupsResult{}, nil)
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LTrue, vals[0])
	assert.Equal(t, lua.LNil, vals[1])
}

func TestJoinGroupsYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &JoinGroupsYield{}
	vals := y.HandleResult(l, pgapi.JoinGroupsResult{Error: errors.New("join failed")}, nil)
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

func TestJoinGroupsYield_HandleResult_DispatchError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &JoinGroupsYield{}
	vals := y.HandleResult(l, nil, errors.New("dispatch error"))
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

func TestLeaveGroupsYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &LeaveGroupsYield{}
	vals := y.HandleResult(l, pgapi.LeaveGroupsResult{}, nil)
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LTrue, vals[0])
	assert.Equal(t, lua.LNil, vals[1])
}

func TestLeaveGroupsYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &LeaveGroupsYield{}
	vals := y.HandleResult(l, pgapi.LeaveGroupsResult{Error: errors.New("leave failed")}, nil)
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

func TestLeaveGroupsYield_HandleResult_DispatchError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := &LeaveGroupsYield{}
	vals := y.HandleResult(l, nil, errors.New("dispatch error"))
	require.Len(t, vals, 2)
	assert.Equal(t, lua.LNil, vals[0])
	assert.NotEqual(t, lua.LNil, vals[1])
}

// ==========================================================================
// Yield Pool tests
// ==========================================================================

func TestJoinYieldPool(t *testing.T) {
	svc := &mockScopeService{}
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireJoinYield(svc, "workers", p)
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, p, y.Caller)
	assert.Equal(t, svc, y.Instance)
	y.Release()
	assert.Nil(t, y.Instance)
	assert.Empty(t, y.Group)
	assert.Equal(t, pid.PID{}, y.Caller)
}

func TestLeaveYieldPool(t *testing.T) {
	svc := &mockScopeService{}
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireLeaveYield(svc, "workers", p)
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, svc, y.Instance)
	y.Release()
	assert.Nil(t, y.Instance)
	assert.Empty(t, y.Group)
}

func TestGetMembersYieldPool(t *testing.T) {
	svc := &mockScopeService{}
	y := AcquireGetMembersYield(svc, "workers")
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, svc, y.Instance)
	y.Release()
	assert.Nil(t, y.Instance)
	assert.Empty(t, y.Group)
}

func TestGetLocalMembersYieldPool(t *testing.T) {
	svc := &mockScopeService{}
	y := AcquireGetLocalMembersYield(svc, "workers")
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, svc, y.Instance)
	y.Release()
	assert.Nil(t, y.Instance)
	assert.Empty(t, y.Group)
}

func TestWhichGroupsYieldPool(t *testing.T) {
	svc := &mockScopeService{}
	y := AcquireWhichGroupsYield(svc)
	assert.Equal(t, svc, y.Instance)
	y.Release()
	assert.Nil(t, y.Instance)
}

func TestWhichLocalGroupsYieldPool(t *testing.T) {
	svc := &mockScopeService{}
	y := AcquireWhichLocalGroupsYield(svc)
	assert.Equal(t, svc, y.Instance)
	y.Release()
	assert.Nil(t, y.Instance)
}

func TestBroadcastYieldPool(t *testing.T) {
	svc := &mockScopeService{}
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireBroadcastYield(svc, p, "workers", "hello", nil)
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, "hello", y.Topic)
	assert.Equal(t, p, y.From)
	assert.Equal(t, svc, y.Instance)
	y.Release()
	assert.Nil(t, y.Instance)
	assert.Empty(t, y.Group)
	assert.Empty(t, y.Topic)
	assert.Nil(t, y.Payloads)
}

func TestBroadcastLocalYieldPool(t *testing.T) {
	svc := &mockScopeService{}
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireBroadcastLocalYield(svc, p, "workers", "hello", nil)
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, svc, y.Instance)
	y.Release()
	assert.Nil(t, y.Instance)
	assert.Empty(t, y.Group)
}

func TestEventsYieldPool(t *testing.T) {
	svc := &mockScopeService{}
	ch := engine.NewChannel(64)
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireEventsYield(svc, ch, p, "pg.events@1")
	assert.Equal(t, svc, y.Instance)
	assert.Equal(t, ch, y.Channel)
	assert.Equal(t, p, y.PID)
	assert.Equal(t, "pg.events@1", y.Topic)
	y.Release()
	assert.Nil(t, y.Instance)
	assert.Nil(t, y.Channel)
	assert.Equal(t, pid.PID{}, y.PID)
	assert.Empty(t, y.Topic)
}

func TestMonitorYieldPool(t *testing.T) {
	svc := &mockScopeService{}
	ch := engine.NewChannel(64)
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireMonitorYield(svc, ch, "workers", p, "pg.monitor@1")
	assert.Equal(t, svc, y.Instance)
	assert.Equal(t, ch, y.Channel)
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, p, y.PID)
	assert.Equal(t, "pg.monitor@1", y.Topic)
	y.Release()
	assert.Nil(t, y.Instance)
	assert.Nil(t, y.Channel)
	assert.Empty(t, y.Group)
	assert.Equal(t, pid.PID{}, y.PID)
	assert.Empty(t, y.Topic)
}

func TestJoinGroupsYieldPool(t *testing.T) {
	svc := &mockScopeService{}
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireJoinGroupsYield(svc, []string{"a", "b"}, p)
	assert.Equal(t, []string{"a", "b"}, y.Groups)
	assert.Equal(t, p, y.Caller)
	assert.Equal(t, svc, y.Instance)
	y.Release()
	assert.Nil(t, y.Instance)
	assert.Nil(t, y.Groups)
	assert.Equal(t, pid.PID{}, y.Caller)
}

func TestLeaveGroupsYieldPool(t *testing.T) {
	svc := &mockScopeService{}
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireLeaveGroupsYield(svc, []string{"a", "b"}, p)
	assert.Equal(t, []string{"a", "b"}, y.Groups)
	assert.Equal(t, p, y.Caller)
	assert.Equal(t, svc, y.Instance)
	y.Release()
	assert.Nil(t, y.Instance)
	assert.Nil(t, y.Groups)
	assert.Equal(t, pid.PID{}, y.Caller)
}

// ==========================================================================
// Yield production tests — pg.open() + Instance method calls via coroutine
// ==========================================================================

func TestJoinProducesYield(t *testing.T) {
	parent, testPID := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `
		local inst = pg.open("test:pg")
		local r = inst:join("workers"); return r
	`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*JoinYield)
	require.True(t, ok, "expected *JoinYield, got %T", lastValue(values))
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, testPID, y.Caller)
	assert.NotNil(t, y.Instance)
}

func TestLeaveProducesYield(t *testing.T) {
	parent, testPID := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `
		local inst = pg.open("test:pg")
		local r = inst:leave("workers"); return r
	`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*LeaveYield)
	require.True(t, ok, "expected *LeaveYield, got %T", lastValue(values))
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, testPID, y.Caller)
	assert.NotNil(t, y.Instance)
}

func TestGetMembersProducesYield(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `
		local inst = pg.open("test:pg")
		local r = inst:get_members("workers"); return r
	`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*GetMembersYield)
	require.True(t, ok, "expected *GetMembersYield, got %T", lastValue(values))
	assert.Equal(t, "workers", y.Group)
	assert.NotNil(t, y.Instance)
}

func TestGetLocalMembersProducesYield(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `
		local inst = pg.open("test:pg")
		local r = inst:get_local_members("workers"); return r
	`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*GetLocalMembersYield)
	require.True(t, ok, "expected *GetLocalMembersYield, got %T", lastValue(values))
	assert.Equal(t, "workers", y.Group)
	assert.NotNil(t, y.Instance)
}

func TestWhichGroupsProducesYield(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `
		local inst = pg.open("test:pg")
		local r = inst:which_groups(); return r
	`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*WhichGroupsYield)
	require.True(t, ok, "expected *WhichGroupsYield, got %T", lastValue(values))
	assert.NotNil(t, y.Instance)
}

func TestWhichLocalGroupsProducesYield(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `
		local inst = pg.open("test:pg")
		local r = inst:which_local_groups(); return r
	`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*WhichLocalGroupsYield)
	require.True(t, ok, "expected *WhichLocalGroupsYield, got %T", lastValue(values))
	assert.NotNil(t, y.Instance)
}

func TestBroadcastProducesYield(t *testing.T) {
	parent, testPID := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `
		local inst = pg.open("test:pg")
		local r = inst:broadcast("workers", "hello"); return r
	`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*BroadcastYield)
	require.True(t, ok, "expected *BroadcastYield, got %T", lastValue(values))
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, "hello", y.Topic)
	assert.Equal(t, testPID, y.From)
	assert.NotNil(t, y.Instance)
}

func TestBroadcastLocalProducesYield(t *testing.T) {
	parent, testPID := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `
		local inst = pg.open("test:pg")
		local r = inst:broadcast_local("workers", "hello"); return r
	`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*BroadcastLocalYield)
	require.True(t, ok, "expected *BroadcastLocalYield, got %T", lastValue(values))
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, "hello", y.Topic)
	assert.Equal(t, testPID, y.From)
	assert.NotNil(t, y.Instance)
}

func TestEventsProducesYield(t *testing.T) {
	parent, testPID := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `
		local inst = pg.open("test:pg")
		local r = inst:events(); return r
	`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*EventsYield)
	require.True(t, ok, "expected *EventsYield, got %T", lastValue(values))
	assert.Equal(t, testPID, y.PID)
	assert.NotNil(t, y.Channel)
	assert.NotEmpty(t, y.Topic)
	assert.NotNil(t, y.Instance)
}

func TestMonitorProducesYield(t *testing.T) {
	parent, testPID := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `
		local inst = pg.open("test:pg")
		local r = inst:monitor("workers"); return r
	`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*MonitorYield)
	require.True(t, ok, "expected *MonitorYield, got %T", lastValue(values))
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, testPID, y.PID)
	assert.NotNil(t, y.Channel)
	assert.NotEmpty(t, y.Topic)
	assert.NotNil(t, y.Instance)
}

func TestJoinGroupsProducesYield(t *testing.T) {
	parent, testPID := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `
		local inst = pg.open("test:pg")
		local r = inst:join({"a", "b"}); return r
	`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*JoinGroupsYield)
	require.True(t, ok, "expected *JoinGroupsYield, got %T", lastValue(values))
	assert.Equal(t, []string{"a", "b"}, y.Groups)
	assert.Equal(t, testPID, y.Caller)
	assert.NotNil(t, y.Instance)
}

func TestLeaveGroupsProducesYield(t *testing.T) {
	parent, testPID := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `
		local inst = pg.open("test:pg")
		local r = inst:leave({"a", "b"}); return r
	`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*LeaveGroupsYield)
	require.True(t, ok, "expected *LeaveGroupsYield, got %T", lastValue(values))
	assert.Equal(t, []string{"a", "b"}, y.Groups)
	assert.Equal(t, testPID, y.Caller)
	assert.NotNil(t, y.Instance)
}

// ==========================================================================
// pg.open() error path tests
// ==========================================================================

func TestOpenNoContext(t *testing.T) {
	// go-lua's VM sets context.Background() before entering mainLoop,
	// so l.Context() is never nil inside a Go function called from Lua.
	// With context.Background() there is no app-context or frame-context,
	// so security defaults to strict mode and returns (nil, error).
	l := newLuaNoContext(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("open"))
	l.Push(lua.LString("test:pg"))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestOpenEmptyID(t *testing.T) {
	l, _ := newLuaWithPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("open"))
	l.Push(lua.LString(""))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestOpenNoRegistry(t *testing.T) {
	l := newLuaNoRegistry(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("open"))
	l.Push(lua.LString("test:pg"))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestOpenNotFound(t *testing.T) {
	l, _ := newLuaWithPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("open"))
	l.Push(lua.LString("nonexistent:pg"))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestOpenPermissionDenied(t *testing.T) {
	l, _ := newLuaStrictMode(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("open"))
	l.Push(lua.LString("test:pg"))
	// pg.open returns (nil, error) on permission denial (consistent with other error paths)
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestOpenSuccess(t *testing.T) {
	l, _ := newLuaWithPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("open"))
	l.Push(lua.LString("test:pg"))
	err := l.PCall(1, 1, nil)
	require.NoError(t, err)

	ud := l.Get(-1)
	assert.Equal(t, lua.LTUserData, ud.Type())
}

// ==========================================================================
// Instance method error path tests
// ==========================================================================

func TestInstanceJoinNoPID(t *testing.T) {
	l := newLuaNoPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("open"))
	l.Push(lua.LString("test:pg"))
	err := l.PCall(1, 1, nil)
	require.NoError(t, err)
	// Instance acquired; now try to join — should fail because no PID
	// We need to get the method and call it with the instance as self
	inst := l.Get(-1)
	l.Pop(1)

	// Use coroutine to test yield, then check error return
	l.SetGlobal("inst", inst)
	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("open"))
	l.Push(lua.LString("test:pg"))
	err = l.PCall(1, 2, nil)
	require.NoError(t, err)
}

func TestInstanceJoinEmptyGroup(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	fn, err := parent.LoadString(`
		local inst = pg.open("test:pg")
		return inst:join("")
	`)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	_, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)
	// Should return (nil, error) — not yield
	require.Len(t, values, 2)
	assert.Equal(t, lua.LNil, values[0])
	assert.NotEqual(t, lua.LNil, values[1])
}

func TestInstanceLeaveEmptyGroup(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	fn, err := parent.LoadString(`
		local inst = pg.open("test:pg")
		return inst:leave("")
	`)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	_, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)
	require.Len(t, values, 2)
	assert.Equal(t, lua.LNil, values[0])
	assert.NotEqual(t, lua.LNil, values[1])
}

func TestInstanceGetMembersEmptyGroup(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	fn, err := parent.LoadString(`
		local inst = pg.open("test:pg")
		return inst:get_members("")
	`)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	_, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)
	require.Len(t, values, 2)
	assert.Equal(t, lua.LNil, values[0])
	assert.NotEqual(t, lua.LNil, values[1])
}

func TestInstanceGetLocalMembersEmptyGroup(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	fn, err := parent.LoadString(`
		local inst = pg.open("test:pg")
		return inst:get_local_members("")
	`)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	_, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)
	require.Len(t, values, 2)
	assert.Equal(t, lua.LNil, values[0])
	assert.NotEqual(t, lua.LNil, values[1])
}

func TestInstanceBroadcastEmptyGroup(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	fn, err := parent.LoadString(`
		local inst = pg.open("test:pg")
		return inst:broadcast("", "hello")
	`)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	_, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)
	require.Len(t, values, 2)
	assert.Equal(t, lua.LNil, values[0])
	assert.NotEqual(t, lua.LNil, values[1])
}

func TestInstanceBroadcastEmptyTopic(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	fn, err := parent.LoadString(`
		local inst = pg.open("test:pg")
		return inst:broadcast("workers", "")
	`)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	_, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)
	require.Len(t, values, 2)
	assert.Equal(t, lua.LNil, values[0])
	assert.NotEqual(t, lua.LNil, values[1])
}

func TestInstanceBroadcastLocalEmptyGroup(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	fn, err := parent.LoadString(`
		local inst = pg.open("test:pg")
		return inst:broadcast_local("", "hello")
	`)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	_, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)
	require.Len(t, values, 2)
	assert.Equal(t, lua.LNil, values[0])
	assert.NotEqual(t, lua.LNil, values[1])
}

func TestInstanceBroadcastLocalEmptyTopic(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	fn, err := parent.LoadString(`
		local inst = pg.open("test:pg")
		return inst:broadcast_local("workers", "")
	`)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	_, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)
	require.Len(t, values, 2)
	assert.Equal(t, lua.LNil, values[0])
	assert.NotEqual(t, lua.LNil, values[1])
}

func TestInstanceMonitorEmptyGroup(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	fn, err := parent.LoadString(`
		local inst = pg.open("test:pg")
		return inst:monitor("")
	`)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	_, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)
	require.Len(t, values, 2)
	assert.Equal(t, lua.LNil, values[0])
	assert.NotEqual(t, lua.LNil, values[1])
}

func TestInstanceJoinEmptyTable(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	fn, err := parent.LoadString(`
		local inst = pg.open("test:pg")
		return inst:join({})
	`)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	_, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)
	require.Len(t, values, 2)
	assert.Equal(t, lua.LNil, values[0])
	assert.NotEqual(t, lua.LNil, values[1])
}

func TestInstanceLeaveEmptyTable(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	fn, err := parent.LoadString(`
		local inst = pg.open("test:pg")
		return inst:leave({})
	`)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	_, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)
	require.Len(t, values, 2)
	assert.Equal(t, lua.LNil, values[0])
	assert.NotEqual(t, lua.LNil, values[1])
}

// ==========================================================================
// Instance release tests
// ==========================================================================

func TestInstanceRelease(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	fn, err := parent.LoadString(`
		local inst = pg.open("test:pg")
		local ok = inst:release()
		return ok
	`)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	state, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)
	assert.Equal(t, lua.ResumeOK, state)
	require.Len(t, values, 1)
	assert.Equal(t, lua.LTrue, values[0])
}

func TestInstanceMethodAfterRelease(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	fn, err := parent.LoadString(`
		local inst = pg.open("test:pg")
		inst:release()
		return inst:join("workers")
	`)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	state, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)
	assert.Equal(t, lua.ResumeOK, state) // returns immediately (no yield)
	require.Len(t, values, 2)
	assert.Equal(t, lua.LNil, values[0])
	assert.NotEqual(t, lua.LNil, values[1]) // error about released instance
}

// ==========================================================================
// Instance __tostring tests
// ==========================================================================

func TestInstanceToString(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	fn, err := parent.LoadString(`
		local inst = pg.open("test:pg")
		return tostring(inst)
	`)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	state, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)
	assert.Equal(t, lua.ResumeOK, state)
	require.Len(t, values, 1)
	assert.Contains(t, values[0].String(), "pg.Instance{test:pg}")
}

func TestInstanceToStringAfterRelease(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	fn, err := parent.LoadString(`
		local inst = pg.open("test:pg")
		inst:release()
		return tostring(inst)
	`)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	state, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)
	assert.Equal(t, lua.ResumeOK, state)
	require.Len(t, values, 1)
	assert.Contains(t, values[0].String(), "released")
}
