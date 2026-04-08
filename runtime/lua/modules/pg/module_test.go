// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
)

// --- test helpers ---

func bindPG(l *lua.LState) {
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
}

func newLuaWithPID(t *testing.T) (*lua.LState, pid.PID) {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	bindPG(l)

	testPID := pid.PID{Host: "h1", UniqID: "p1"}
	ctx := ctxapi.NewRootContext()
	security.SetStrictMode(ctx, false)
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	require.NoError(t, runtimeapi.SetFramePID(ctx, testPID))
	l.SetContext(ctx)

	return l, testPID
}

func newLuaNoContext(t *testing.T) *lua.LState {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	bindPG(l)
	// No context set — l.Context() returns nil
	return l
}

func newLuaNoPID(t *testing.T) *lua.LState {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	bindPG(l)

	ctx := ctxapi.NewRootContext()
	security.SetStrictMode(ctx, false)
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	// No PID set in frame
	l.SetContext(ctx)

	return l
}

// --- Module build tests ---

func TestModuleInfo(t *testing.T) {
	info := Module.Info()
	assert.Equal(t, "pg", info.Name)
	assert.NotEmpty(t, info.Description)
}

func TestModuleBuild(t *testing.T) {
	tbl, yields := Module.Build()
	require.NotNil(t, tbl)
	require.NotNil(t, yields)
	assert.Len(t, yields, 7)
}

func TestModuleFunctions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindPG(l)

	mod := l.GetGlobal("pg")
	require.Equal(t, lua.LTTable, mod.Type())

	modTbl := mod.(*lua.LTable)
	functions := []string{
		"join", "leave", "get_members", "get_local_members",
		"which_groups", "broadcast", "broadcast_local",
	}
	for _, fn := range functions {
		assert.Equal(t, lua.LTFunction, modTbl.RawGetString(fn).Type(), "function %s not registered", fn)
	}
}

func TestModuleImmutable(t *testing.T) {
	tbl, _ := Module.Build()
	assert.True(t, tbl.Immutable)
}

// --- Yield CmdID tests ---

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

func TestBroadcastYieldCmdID(t *testing.T) {
	y := &BroadcastYield{}
	assert.Equal(t, pgapi.Broadcast, y.CmdID())
}

func TestBroadcastLocalYieldCmdID(t *testing.T) {
	y := &BroadcastLocalYield{}
	assert.Equal(t, pgapi.BroadcastLocal, y.CmdID())
}

// --- Yield String/Type tests ---

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
		{"Broadcast", &BroadcastYield{}, "<pg_broadcast_yield>"},
		{"BroadcastLocal", &BroadcastLocalYield{}, "<pg_broadcast_local_yield>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.yield.String())
			assert.Equal(t, lua.LTUserData, tt.yield.Type())
		})
	}
}

// --- Yield ToCommand tests ---

func TestJoinYieldToCommand(t *testing.T) {
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireJoinYield("workers", p)

	cmd := y.ToCommand()
	joinCmd, ok := cmd.(*pgapi.JoinCmd)
	require.True(t, ok)
	assert.Equal(t, "workers", joinCmd.Group)
	assert.Equal(t, p, joinCmd.Caller)
	assert.Equal(t, pgapi.Join, joinCmd.CmdID())

	joinCmd.Release()
	y.Release()
}

func TestLeaveYieldToCommand(t *testing.T) {
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireLeaveYield("workers", p)

	cmd := y.ToCommand()
	leaveCmd, ok := cmd.(*pgapi.LeaveCmd)
	require.True(t, ok)
	assert.Equal(t, "workers", leaveCmd.Group)
	assert.Equal(t, p, leaveCmd.Caller)
	assert.Equal(t, pgapi.Leave, leaveCmd.CmdID())

	leaveCmd.Release()
	y.Release()
}

func TestGetMembersYieldToCommand(t *testing.T) {
	y := AcquireGetMembersYield("workers")

	cmd := y.ToCommand()
	gmc, ok := cmd.(*pgapi.GetMembersCmd)
	require.True(t, ok)
	assert.Equal(t, "workers", gmc.Group)
	assert.Equal(t, pgapi.GetMembers, gmc.CmdID())

	gmc.Release()
	y.Release()
}

func TestGetLocalMembersYieldToCommand(t *testing.T) {
	y := AcquireGetLocalMembersYield("workers")

	cmd := y.ToCommand()
	glmc, ok := cmd.(*pgapi.GetLocalMembersCmd)
	require.True(t, ok)
	assert.Equal(t, "workers", glmc.Group)
	assert.Equal(t, pgapi.GetLocalMembers, glmc.CmdID())

	glmc.Release()
	y.Release()
}

func TestWhichGroupsYieldToCommand(t *testing.T) {
	y := AcquireWhichGroupsYield()

	cmd := y.ToCommand()
	wgc, ok := cmd.(*pgapi.WhichGroupsCmd)
	require.True(t, ok)
	assert.Equal(t, pgapi.WhichGroups, wgc.CmdID())

	wgc.Release()
	y.Release()
}

func TestBroadcastYieldToCommand(t *testing.T) {
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireBroadcastYield(p, "workers", "hello", nil)

	cmd := y.ToCommand()
	bc, ok := cmd.(*pgapi.BroadcastCmd)
	require.True(t, ok)
	assert.Equal(t, p, bc.From)
	assert.Equal(t, "workers", bc.Group)
	assert.Equal(t, "hello", bc.Topic)
	assert.Equal(t, pgapi.Broadcast, bc.CmdID())

	bc.Release()
	y.Release()
}

func TestBroadcastLocalYieldToCommand(t *testing.T) {
	p := pid.PID{Host: "h1", UniqID: "p1"}
	y := AcquireBroadcastLocalYield(p, "workers", "hello", nil)

	cmd := y.ToCommand()
	blc, ok := cmd.(*pgapi.BroadcastLocalCmd)
	require.True(t, ok)
	assert.Equal(t, p, blc.From)
	assert.Equal(t, "workers", blc.Group)
	assert.Equal(t, "hello", blc.Topic)
	assert.Equal(t, pgapi.BroadcastLocal, blc.CmdID())

	blc.Release()
	y.Release()
}

// --- Yield HandleResult tests ---

func TestJoinYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &JoinYield{}
	result := y.HandleResult(l, pgapi.JoinResult{Error: nil}, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LTrue, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestJoinYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &JoinYield{}
	result := y.HandleResult(l, nil, errors.New("dispatch error"))
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestJoinYield_HandleResult_JoinError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &JoinYield{}
	result := y.HandleResult(l, pgapi.JoinResult{Error: errors.New("join failed")}, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestLeaveYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &LeaveYield{}
	result := y.HandleResult(l, pgapi.LeaveResult{Error: nil}, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LTrue, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestLeaveYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &LeaveYield{}
	result := y.HandleResult(l, pgapi.LeaveResult{Error: errors.New("not joined")}, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestGetMembersYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	p1 := pid.PID{Host: "h1", UniqID: "1"}
	p2 := pid.PID{Host: "h1", UniqID: "2"}

	y := &GetMembersYield{}
	result := y.HandleResult(l, pgapi.GetMembersResult{Members: []pid.PID{p1, p2}}, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[1])

	tbl, ok := result[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 2, tbl.Len())
}

func TestGetMembersYield_HandleResult_Empty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &GetMembersYield{}
	result := y.HandleResult(l, pgapi.GetMembersResult{Members: nil}, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[1])

	tbl, ok := result[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 0, tbl.Len())
}

func TestGetMembersYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &GetMembersYield{}
	result := y.HandleResult(l, nil, errors.New("get failed"))
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestGetMembersYield_HandleResult_WrongType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &GetMembersYield{}
	result := y.HandleResult(l, "not a result", nil)
	require.Len(t, result, 2)
	// Should return empty table on wrong type
	tbl, ok := result[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 0, tbl.Len())
}

func TestGetLocalMembersYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	p1 := pid.PID{Host: "h1", UniqID: "1"}
	y := &GetLocalMembersYield{}
	result := y.HandleResult(l, pgapi.GetLocalMembersResult{Members: []pid.PID{p1}}, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[1])

	tbl, ok := result[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 1, tbl.Len())
}

func TestWhichGroupsYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &WhichGroupsYield{}
	result := y.HandleResult(l, pgapi.WhichGroupsResult{Groups: []string{"workers", "managers"}}, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[1])

	tbl, ok := result[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 2, tbl.Len())
	assert.Equal(t, "workers", string(tbl.RawGetInt(1).(lua.LString)))
	assert.Equal(t, "managers", string(tbl.RawGetInt(2).(lua.LString)))
}

func TestWhichGroupsYield_HandleResult_Empty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &WhichGroupsYield{}
	result := y.HandleResult(l, pgapi.WhichGroupsResult{Groups: nil}, nil)
	require.Len(t, result, 2)
	tbl, ok := result[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 0, tbl.Len())
}

func TestBroadcastYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &BroadcastYield{}
	result := y.HandleResult(l, pgapi.BroadcastResult{Sent: 3}, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LTrue, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestBroadcastYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &BroadcastYield{}
	result := y.HandleResult(l, pgapi.BroadcastResult{Error: errors.New("fail")}, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestBroadcastLocalYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &BroadcastLocalYield{}
	result := y.HandleResult(l, pgapi.BroadcastLocalResult{Sent: 1}, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LTrue, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestBroadcastLocalYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &BroadcastLocalYield{}
	result := y.HandleResult(l, pgapi.BroadcastLocalResult{Error: errors.New("fail")}, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

// --- Pool acquire/release tests ---

func TestJoinYieldPool(t *testing.T) {
	p := pid.PID{Host: "h1", UniqID: "1"}
	y := AcquireJoinYield("workers", p)
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, p, y.Caller)

	ReleaseJoinYield(y)
	// After release, fields should be zeroed
	assert.Equal(t, "", y.Group)
	assert.Equal(t, pid.PID{}, y.Caller)
}

func TestLeaveYieldPool(t *testing.T) {
	p := pid.PID{Host: "h1", UniqID: "1"}
	y := AcquireLeaveYield("workers", p)
	assert.Equal(t, "workers", y.Group)

	ReleaseLeaveYield(y)
	assert.Equal(t, "", y.Group)
	assert.Equal(t, pid.PID{}, y.Caller)
}

func TestGetMembersYieldPool(t *testing.T) {
	y := AcquireGetMembersYield("workers")
	assert.Equal(t, "workers", y.Group)

	ReleaseGetMembersYield(y)
	assert.Equal(t, "", y.Group)
}

func TestGetLocalMembersYieldPool(t *testing.T) {
	y := AcquireGetLocalMembersYield("workers")
	assert.Equal(t, "workers", y.Group)

	ReleaseGetLocalMembersYield(y)
	assert.Equal(t, "", y.Group)
}

func TestWhichGroupsYieldPool(t *testing.T) {
	y := AcquireWhichGroupsYield()
	require.NotNil(t, y)

	ReleaseWhichGroupsYield(y)
}

func TestBroadcastYieldPool(t *testing.T) {
	p := pid.PID{Host: "h1", UniqID: "1"}
	y := AcquireBroadcastYield(p, "workers", "hello", nil)
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, "hello", y.Topic)
	assert.Equal(t, p, y.From)

	ReleaseBroadcastYield(y)
	assert.Equal(t, "", y.Group)
	assert.Equal(t, "", y.Topic)
	assert.Equal(t, pid.PID{}, y.From)
	assert.Nil(t, y.Payloads)
}

func TestBroadcastLocalYieldPool(t *testing.T) {
	p := pid.PID{Host: "h1", UniqID: "1"}
	y := AcquireBroadcastLocalYield(p, "workers", "hello", nil)
	assert.Equal(t, "workers", y.Group)

	ReleaseBroadcastLocalYield(y)
	assert.Equal(t, "", y.Group)
	assert.Equal(t, "", y.Topic)
	assert.Equal(t, pid.PID{}, y.From)
	assert.Nil(t, y.Payloads)
}

// --- Module function error path tests ---

func TestJoinNoContext(t *testing.T) {
	l := newLuaNoContext(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("join"))
	l.Push(lua.LString("workers"))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1)) // error
}

func TestJoinNoPID(t *testing.T) {
	l := newLuaNoPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("join"))
	l.Push(lua.LString("workers"))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestLeaveNoContext(t *testing.T) {
	l := newLuaNoContext(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("leave"))
	l.Push(lua.LString("workers"))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestLeaveNoPID(t *testing.T) {
	l := newLuaNoPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("leave"))
	l.Push(lua.LString("workers"))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestGetMembersNoContext(t *testing.T) {
	l := newLuaNoContext(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("get_members"))
	l.Push(lua.LString("workers"))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestWhichGroupsNoContext(t *testing.T) {
	l := newLuaNoContext(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("which_groups"))
	err := l.PCall(0, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestBroadcastNoContext(t *testing.T) {
	l := newLuaNoContext(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("broadcast"))
	l.Push(lua.LString("workers"))
	l.Push(lua.LString("hello"))
	err := l.PCall(2, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestBroadcastLocalNoContext(t *testing.T) {
	l := newLuaNoContext(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("broadcast_local"))
	l.Push(lua.LString("workers"))
	l.Push(lua.LString("hello"))
	err := l.PCall(2, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

// --- Yield path tests ---
// These tests verify that each Lua function produces the correct yield type
// when called with valid context/PID. We use coroutine Resume to properly
// handle the yield (-1 return) from Go functions.

// resumeYield compiles a Lua snippet, creates a coroutine thread with the
// parent's context, resumes it, and returns the resume state and yielded values.
// The yield value is always the last element in the returned values slice
// (preceding values are function arguments left on the stack).
func resumeYield(t *testing.T, parent *lua.LState, script string) (lua.ResumeState, []lua.LValue) {
	t.Helper()
	fn, err := parent.LoadString(script)
	require.NoError(t, err)

	thread := parent.NewThreadWithContext(parent.Context())
	state, values, err := parent.Resume(thread, fn)
	require.NoError(t, err)

	return state, values
}

// lastValue returns the last element from a values slice — the yield struct
// pushed by the Go function sits at the end of the stack.
func lastValue(values []lua.LValue) lua.LValue {
	if len(values) == 0 {
		return nil
	}
	return values[len(values)-1]
}

func TestJoinProducesYield(t *testing.T) {
	parent, testPID := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `local r = pg.join("workers"); return r`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*JoinYield)
	require.True(t, ok, "expected *JoinYield, got %T", lastValue(values))
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, testPID, y.Caller)
	assert.Equal(t, pgapi.Join, y.CmdID())
}

func TestLeaveProducesYield(t *testing.T) {
	parent, testPID := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `local r = pg.leave("workers"); return r`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*LeaveYield)
	require.True(t, ok, "expected *LeaveYield, got %T", lastValue(values))
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, testPID, y.Caller)
	assert.Equal(t, pgapi.Leave, y.CmdID())
}

func TestGetMembersProducesYield(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `local r = pg.get_members("workers"); return r`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*GetMembersYield)
	require.True(t, ok, "expected *GetMembersYield, got %T", lastValue(values))
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, pgapi.GetMembers, y.CmdID())
}

func TestGetLocalMembersProducesYield(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `local r = pg.get_local_members("workers"); return r`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*GetLocalMembersYield)
	require.True(t, ok, "expected *GetLocalMembersYield, got %T", lastValue(values))
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, pgapi.GetLocalMembers, y.CmdID())
}

func TestWhichGroupsProducesYield(t *testing.T) {
	parent, _ := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `local r = pg.which_groups(); return r`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*WhichGroupsYield)
	require.True(t, ok, "expected *WhichGroupsYield, got %T", lastValue(values))
	assert.Equal(t, pgapi.WhichGroups, y.CmdID())
}

func TestBroadcastProducesYield(t *testing.T) {
	parent, testPID := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `local r = pg.broadcast("workers", "hello", "payload1"); return r`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*BroadcastYield)
	require.True(t, ok, "expected *BroadcastYield, got %T", lastValue(values))
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, "hello", y.Topic)
	assert.Equal(t, testPID, y.From)
	assert.Equal(t, pgapi.Broadcast, y.CmdID())
	require.Len(t, y.Payloads, 1)
}

func TestBroadcastLocalProducesYield(t *testing.T) {
	parent, testPID := newLuaWithPID(t)

	state, values := resumeYield(t, parent, `local r = pg.broadcast_local("workers", "hello"); return r`)
	assert.Equal(t, lua.ResumeYield, state)
	require.NotEmpty(t, values)

	y, ok := lastValue(values).(*BroadcastLocalYield)
	require.True(t, ok, "expected *BroadcastLocalYield, got %T", lastValue(values))
	assert.Equal(t, "workers", y.Group)
	assert.Equal(t, "hello", y.Topic)
	assert.Equal(t, testPID, y.From)
	assert.Equal(t, pgapi.BroadcastLocal, y.CmdID())
	assert.Nil(t, y.Payloads)
}

// --- Missing no-context / no-PID error path tests ---

func TestGetLocalMembersNoContext(t *testing.T) {
	l := newLuaNoContext(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("get_local_members"))
	l.Push(lua.LString("workers"))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestBroadcastNoPID(t *testing.T) {
	l := newLuaNoPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("broadcast"))
	l.Push(lua.LString("workers"))
	l.Push(lua.LString("hello"))
	err := l.PCall(2, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestBroadcastLocalNoPID(t *testing.T) {
	l := newLuaNoPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("broadcast_local"))
	l.Push(lua.LString("workers"))
	l.Push(lua.LString("hello"))
	err := l.PCall(2, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

// --- Empty group name validation tests ---

func TestJoinEmptyGroup(t *testing.T) {
	l, _ := newLuaWithPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("join"))
	l.Push(lua.LString(""))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	errVal := l.Get(-1)
	assert.NotEqual(t, lua.LNil, errVal)
}

func TestLeaveEmptyGroup(t *testing.T) {
	l, _ := newLuaWithPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("leave"))
	l.Push(lua.LString(""))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestGetMembersEmptyGroup(t *testing.T) {
	l, _ := newLuaWithPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("get_members"))
	l.Push(lua.LString(""))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestGetLocalMembersEmptyGroup(t *testing.T) {
	l, _ := newLuaWithPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("get_local_members"))
	l.Push(lua.LString(""))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestBroadcastEmptyGroup(t *testing.T) {
	l, _ := newLuaWithPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("broadcast"))
	l.Push(lua.LString(""))
	l.Push(lua.LString("topic"))
	err := l.PCall(2, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestBroadcastLocalEmptyGroup(t *testing.T) {
	l, _ := newLuaWithPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("broadcast_local"))
	l.Push(lua.LString(""))
	l.Push(lua.LString("topic"))
	err := l.PCall(2, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

// --- Empty topic validation tests for broadcast ---

func TestBroadcastEmptyTopic(t *testing.T) {
	l, _ := newLuaWithPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("broadcast"))
	l.Push(lua.LString("workers"))
	l.Push(lua.LString(""))
	err := l.PCall(2, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestBroadcastLocalEmptyTopic(t *testing.T) {
	l, _ := newLuaWithPID(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("broadcast_local"))
	l.Push(lua.LString("workers"))
	l.Push(lua.LString(""))
	err := l.PCall(2, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

// --- Security permission denied tests (strict mode) ---

func newLuaStrictMode(t *testing.T) (*lua.LState, pid.PID) {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	bindPG(l)

	testPID := pid.PID{Host: "h1", UniqID: "p1"}
	ctx := ctxapi.NewRootContext()
	security.SetStrictMode(ctx, true) // strict mode — no actor/scope = denied
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	require.NoError(t, runtimeapi.SetFramePID(ctx, testPID))
	l.SetContext(ctx)

	return l, testPID
}

func TestJoinPermissionDenied(t *testing.T) {
	l, _ := newLuaStrictMode(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("join"))
	l.Push(lua.LString("workers"))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestLeavePermissionDenied(t *testing.T) {
	l, _ := newLuaStrictMode(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("leave"))
	l.Push(lua.LString("workers"))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestGetMembersPermissionDenied(t *testing.T) {
	l, _ := newLuaStrictMode(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("get_members"))
	l.Push(lua.LString("workers"))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestGetLocalMembersPermissionDenied(t *testing.T) {
	l, _ := newLuaStrictMode(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("get_local_members"))
	l.Push(lua.LString("workers"))
	err := l.PCall(1, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestWhichGroupsPermissionDenied(t *testing.T) {
	l, _ := newLuaStrictMode(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("which_groups"))
	err := l.PCall(0, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestBroadcastPermissionDenied(t *testing.T) {
	l, _ := newLuaStrictMode(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("broadcast"))
	l.Push(lua.LString("workers"))
	l.Push(lua.LString("hello"))
	err := l.PCall(2, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

func TestBroadcastLocalPermissionDenied(t *testing.T) {
	l, _ := newLuaStrictMode(t)

	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("broadcast_local"))
	l.Push(lua.LString("workers"))
	l.Push(lua.LString("hello"))
	err := l.PCall(2, 2, nil)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, l.Get(-2))
	assert.NotEqual(t, lua.LNil, l.Get(-1))
}

// --- Additional HandleResult error path tests ---

func TestLeaveYield_HandleResult_DispatchError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &LeaveYield{}
	result := y.HandleResult(l, nil, errors.New("dispatch failed"))
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestGetLocalMembersYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &GetLocalMembersYield{}
	result := y.HandleResult(l, nil, errors.New("dispatch failed"))
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestGetLocalMembersYield_HandleResult_WrongType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &GetLocalMembersYield{}
	result := y.HandleResult(l, "not a result", nil)
	require.Len(t, result, 2)
	// Should return empty table on wrong type
	tbl, ok := result[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 0, tbl.Len())
}

func TestWhichGroupsYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &WhichGroupsYield{}
	result := y.HandleResult(l, nil, errors.New("dispatch failed"))
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestWhichGroupsYield_HandleResult_WrongType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &WhichGroupsYield{}
	result := y.HandleResult(l, "not a result", nil)
	require.Len(t, result, 2)
	// Should return empty table on wrong type
	tbl, ok := result[0].(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, 0, tbl.Len())
}

func TestBroadcastYield_HandleResult_DispatchError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &BroadcastYield{}
	result := y.HandleResult(l, nil, errors.New("dispatch failed"))
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestBroadcastLocalYield_HandleResult_DispatchError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &BroadcastLocalYield{}
	result := y.HandleResult(l, nil, errors.New("dispatch failed"))
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}
