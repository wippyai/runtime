// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
)

// --- JoinYield ---

type JoinYield struct {
	Group  string
	Caller pid.PID
}

var joinYieldPool = sync.Pool{New: func() any { return &JoinYield{} }}

func AcquireJoinYield(group string, caller pid.PID) *JoinYield {
	y := joinYieldPool.Get().(*JoinYield)
	y.Group = group
	y.Caller = caller
	return y
}

func ReleaseJoinYield(y *JoinYield) {
	y.Group = ""
	y.Caller = pid.PID{}
	joinYieldPool.Put(y)
}

func (y *JoinYield) String() string       { return "<pg_join_yield>" }
func (y *JoinYield) Type() lua.LValueType { return lua.LTUserData }
func (y *JoinYield) CmdID() dispatcher.CommandID {
	return pgapi.Join
}

func (y *JoinYield) ToCommand() dispatcher.Command {
	cmd := pgapi.AcquireJoinCmd()
	cmd.Group = y.Group
	cmd.Caller = y.Caller
	return cmd
}

func (y *JoinYield) Release() { ReleaseJoinYield(y) }

func (y *JoinYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "pg join")}
	}
	if result, ok := data.(pgapi.JoinResult); ok && result.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, result.Error, "pg join")}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// --- LeaveYield ---

type LeaveYield struct {
	Group  string
	Caller pid.PID
}

var leaveYieldPool = sync.Pool{New: func() any { return &LeaveYield{} }}

func AcquireLeaveYield(group string, caller pid.PID) *LeaveYield {
	y := leaveYieldPool.Get().(*LeaveYield)
	y.Group = group
	y.Caller = caller
	return y
}

func ReleaseLeaveYield(y *LeaveYield) {
	y.Group = ""
	y.Caller = pid.PID{}
	leaveYieldPool.Put(y)
}

func (y *LeaveYield) String() string       { return "<pg_leave_yield>" }
func (y *LeaveYield) Type() lua.LValueType { return lua.LTUserData }
func (y *LeaveYield) CmdID() dispatcher.CommandID {
	return pgapi.Leave
}

func (y *LeaveYield) ToCommand() dispatcher.Command {
	cmd := pgapi.AcquireLeaveCmd()
	cmd.Group = y.Group
	cmd.Caller = y.Caller
	return cmd
}

func (y *LeaveYield) Release() { ReleaseLeaveYield(y) }

func (y *LeaveYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "pg leave")}
	}
	if result, ok := data.(pgapi.LeaveResult); ok && result.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, result.Error, "pg leave")}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// --- GetMembersYield ---

type GetMembersYield struct {
	Group string
}

var getMembersYieldPool = sync.Pool{New: func() any { return &GetMembersYield{} }}

func AcquireGetMembersYield(group string) *GetMembersYield {
	y := getMembersYieldPool.Get().(*GetMembersYield)
	y.Group = group
	return y
}

func ReleaseGetMembersYield(y *GetMembersYield) {
	y.Group = ""
	getMembersYieldPool.Put(y)
}

func (y *GetMembersYield) String() string       { return "<pg_get_members_yield>" }
func (y *GetMembersYield) Type() lua.LValueType { return lua.LTUserData }
func (y *GetMembersYield) CmdID() dispatcher.CommandID {
	return pgapi.GetMembers
}

func (y *GetMembersYield) ToCommand() dispatcher.Command {
	cmd := pgapi.AcquireGetMembersCmd()
	cmd.Group = y.Group
	return cmd
}

func (y *GetMembersYield) Release() { ReleaseGetMembersYield(y) }

func (y *GetMembersYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "pg get_members")}
	}
	result, ok := data.(pgapi.GetMembersResult)
	if !ok {
		return []lua.LValue{lua.CreateTable(0, 0), lua.LNil}
	}
	tbl := lua.CreateTable(len(result.Members), 0)
	for i, p := range result.Members {
		tbl.RawSetInt(i+1, lua.LString(p.String()))
	}
	return []lua.LValue{tbl, lua.LNil}
}

// --- GetLocalMembersYield ---

type GetLocalMembersYield struct {
	Group string
}

var getLocalMembersYieldPool = sync.Pool{New: func() any { return &GetLocalMembersYield{} }}

func AcquireGetLocalMembersYield(group string) *GetLocalMembersYield {
	y := getLocalMembersYieldPool.Get().(*GetLocalMembersYield)
	y.Group = group
	return y
}

func ReleaseGetLocalMembersYield(y *GetLocalMembersYield) {
	y.Group = ""
	getLocalMembersYieldPool.Put(y)
}

func (y *GetLocalMembersYield) String() string       { return "<pg_get_local_members_yield>" }
func (y *GetLocalMembersYield) Type() lua.LValueType { return lua.LTUserData }
func (y *GetLocalMembersYield) CmdID() dispatcher.CommandID {
	return pgapi.GetLocalMembers
}

func (y *GetLocalMembersYield) ToCommand() dispatcher.Command {
	cmd := pgapi.AcquireGetLocalMembersCmd()
	cmd.Group = y.Group
	return cmd
}

func (y *GetLocalMembersYield) Release() { ReleaseGetLocalMembersYield(y) }

func (y *GetLocalMembersYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "pg get_local_members")}
	}
	result, ok := data.(pgapi.GetLocalMembersResult)
	if !ok {
		return []lua.LValue{lua.CreateTable(0, 0), lua.LNil}
	}
	tbl := lua.CreateTable(len(result.Members), 0)
	for i, p := range result.Members {
		tbl.RawSetInt(i+1, lua.LString(p.String()))
	}
	return []lua.LValue{tbl, lua.LNil}
}

// --- WhichGroupsYield ---

type WhichGroupsYield struct{}

var whichGroupsYieldPool = sync.Pool{New: func() any { return &WhichGroupsYield{} }}

func AcquireWhichGroupsYield() *WhichGroupsYield {
	return whichGroupsYieldPool.Get().(*WhichGroupsYield)
}

func ReleaseWhichGroupsYield(y *WhichGroupsYield) {
	whichGroupsYieldPool.Put(y)
}

func (y *WhichGroupsYield) String() string       { return "<pg_which_groups_yield>" }
func (y *WhichGroupsYield) Type() lua.LValueType { return lua.LTUserData }
func (y *WhichGroupsYield) CmdID() dispatcher.CommandID {
	return pgapi.WhichGroups
}

func (y *WhichGroupsYield) ToCommand() dispatcher.Command {
	return pgapi.AcquireWhichGroupsCmd()
}

func (y *WhichGroupsYield) Release() { ReleaseWhichGroupsYield(y) }

func (y *WhichGroupsYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "pg which_groups")}
	}
	result, ok := data.(pgapi.WhichGroupsResult)
	if !ok {
		return []lua.LValue{lua.CreateTable(0, 0), lua.LNil}
	}
	tbl := lua.CreateTable(len(result.Groups), 0)
	for i, g := range result.Groups {
		tbl.RawSetInt(i+1, lua.LString(g))
	}
	return []lua.LValue{tbl, lua.LNil}
}

// --- BroadcastYield ---

type BroadcastYield struct {
	From     pid.PID
	Group    string
	Topic    string
	Payloads payload.Payloads
}

var broadcastYieldPool = sync.Pool{New: func() any { return &BroadcastYield{} }}

func AcquireBroadcastYield(from pid.PID, group, topic string, payloads payload.Payloads) *BroadcastYield {
	y := broadcastYieldPool.Get().(*BroadcastYield)
	y.From = from
	y.Group = group
	y.Topic = topic
	y.Payloads = payloads
	return y
}

func ReleaseBroadcastYield(y *BroadcastYield) {
	y.From = pid.PID{}
	y.Group = ""
	y.Topic = ""
	y.Payloads = nil
	broadcastYieldPool.Put(y)
}

func (y *BroadcastYield) String() string       { return "<pg_broadcast_yield>" }
func (y *BroadcastYield) Type() lua.LValueType { return lua.LTUserData }
func (y *BroadcastYield) CmdID() dispatcher.CommandID {
	return pgapi.Broadcast
}

func (y *BroadcastYield) ToCommand() dispatcher.Command {
	cmd := pgapi.AcquireBroadcastCmd()
	cmd.From = y.From
	cmd.Group = y.Group
	cmd.Topic = y.Topic
	cmd.Payloads = y.Payloads
	return cmd
}

func (y *BroadcastYield) Release() { ReleaseBroadcastYield(y) }

func (y *BroadcastYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "pg broadcast")}
	}
	if result, ok := data.(pgapi.BroadcastResult); ok && result.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, result.Error, "pg broadcast")}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// --- BroadcastLocalYield ---

type BroadcastLocalYield struct {
	From     pid.PID
	Group    string
	Topic    string
	Payloads payload.Payloads
}

var broadcastLocalYieldPool = sync.Pool{New: func() any { return &BroadcastLocalYield{} }}

func AcquireBroadcastLocalYield(from pid.PID, group, topic string, payloads payload.Payloads) *BroadcastLocalYield {
	y := broadcastLocalYieldPool.Get().(*BroadcastLocalYield)
	y.From = from
	y.Group = group
	y.Topic = topic
	y.Payloads = payloads
	return y
}

func ReleaseBroadcastLocalYield(y *BroadcastLocalYield) {
	y.From = pid.PID{}
	y.Group = ""
	y.Topic = ""
	y.Payloads = nil
	broadcastLocalYieldPool.Put(y)
}

func (y *BroadcastLocalYield) String() string       { return "<pg_broadcast_local_yield>" }
func (y *BroadcastLocalYield) Type() lua.LValueType { return lua.LTUserData }
func (y *BroadcastLocalYield) CmdID() dispatcher.CommandID {
	return pgapi.BroadcastLocal
}

func (y *BroadcastLocalYield) ToCommand() dispatcher.Command {
	cmd := pgapi.AcquireBroadcastLocalCmd()
	cmd.From = y.From
	cmd.Group = y.Group
	cmd.Topic = y.Topic
	cmd.Payloads = y.Payloads
	return cmd
}

func (y *BroadcastLocalYield) Release() { ReleaseBroadcastLocalYield(y) }

func (y *BroadcastLocalYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "pg broadcast_local")}
	}
	if result, ok := data.(pgapi.BroadcastLocalResult); ok && result.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, result.Error, "pg broadcast_local")}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}
