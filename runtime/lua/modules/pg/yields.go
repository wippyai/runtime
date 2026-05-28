// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"errors"
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

var errUnexpectedResult = errors.New("pg: unexpected monitor result type")

// --- JoinYield ---

type JoinYield struct {
	Instance pgapi.ScopeService
	Group    string
	Caller   pid.PID
}

var joinYieldPool = sync.Pool{New: func() any { return &JoinYield{} }}

func AcquireJoinYield(instance pgapi.ScopeService, group string, caller pid.PID) *JoinYield {
	y := joinYieldPool.Get().(*JoinYield)
	y.Instance = instance
	y.Group = group
	y.Caller = caller
	return y
}

func ReleaseJoinYield(y *JoinYield) {
	y.Instance = nil
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
	cmd.Instance = y.Instance
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

// --- JoinGroupsYield ---

type JoinGroupsYield struct {
	Instance pgapi.ScopeService
	Caller   pid.PID
	Groups   []string
}

var joinGroupsYieldPool = sync.Pool{New: func() any { return &JoinGroupsYield{} }}

func AcquireJoinGroupsYield(instance pgapi.ScopeService, groups []string, caller pid.PID) *JoinGroupsYield {
	y := joinGroupsYieldPool.Get().(*JoinGroupsYield)
	y.Instance = instance
	y.Groups = groups
	y.Caller = caller
	return y
}

func ReleaseJoinGroupsYield(y *JoinGroupsYield) {
	y.Instance = nil
	y.Groups = nil
	y.Caller = pid.PID{}
	joinGroupsYieldPool.Put(y)
}

func (y *JoinGroupsYield) String() string       { return "<pg_join_groups_yield>" }
func (y *JoinGroupsYield) Type() lua.LValueType { return lua.LTUserData }
func (y *JoinGroupsYield) CmdID() dispatcher.CommandID {
	return pgapi.JoinGroups
}

func (y *JoinGroupsYield) ToCommand() dispatcher.Command {
	cmd := pgapi.AcquireJoinGroupsCmd()
	cmd.Instance = y.Instance
	cmd.Groups = y.Groups
	cmd.Caller = y.Caller
	return cmd
}

func (y *JoinGroupsYield) Release() { ReleaseJoinGroupsYield(y) }

func (y *JoinGroupsYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "pg join")}
	}
	if result, ok := data.(pgapi.JoinGroupsResult); ok && result.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, result.Error, "pg join")}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// --- LeaveYield ---

type LeaveYield struct {
	Instance pgapi.ScopeService
	Group    string
	Caller   pid.PID
}

var leaveYieldPool = sync.Pool{New: func() any { return &LeaveYield{} }}

func AcquireLeaveYield(instance pgapi.ScopeService, group string, caller pid.PID) *LeaveYield {
	y := leaveYieldPool.Get().(*LeaveYield)
	y.Instance = instance
	y.Group = group
	y.Caller = caller
	return y
}

func ReleaseLeaveYield(y *LeaveYield) {
	y.Instance = nil
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
	cmd.Instance = y.Instance
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

// --- LeaveGroupsYield ---

type LeaveGroupsYield struct {
	Instance pgapi.ScopeService
	Caller   pid.PID
	Groups   []string
}

var leaveGroupsYieldPool = sync.Pool{New: func() any { return &LeaveGroupsYield{} }}

func AcquireLeaveGroupsYield(instance pgapi.ScopeService, groups []string, caller pid.PID) *LeaveGroupsYield {
	y := leaveGroupsYieldPool.Get().(*LeaveGroupsYield)
	y.Instance = instance
	y.Groups = groups
	y.Caller = caller
	return y
}

func ReleaseLeaveGroupsYield(y *LeaveGroupsYield) {
	y.Instance = nil
	y.Groups = nil
	y.Caller = pid.PID{}
	leaveGroupsYieldPool.Put(y)
}

func (y *LeaveGroupsYield) String() string       { return "<pg_leave_groups_yield>" }
func (y *LeaveGroupsYield) Type() lua.LValueType { return lua.LTUserData }
func (y *LeaveGroupsYield) CmdID() dispatcher.CommandID {
	return pgapi.LeaveGroups
}

func (y *LeaveGroupsYield) ToCommand() dispatcher.Command {
	cmd := pgapi.AcquireLeaveGroupsCmd()
	cmd.Instance = y.Instance
	cmd.Groups = y.Groups
	cmd.Caller = y.Caller
	return cmd
}

func (y *LeaveGroupsYield) Release() { ReleaseLeaveGroupsYield(y) }

func (y *LeaveGroupsYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "pg leave")}
	}
	if result, ok := data.(pgapi.LeaveGroupsResult); ok && result.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, result.Error, "pg leave")}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// --- GetMembersYield ---

type GetMembersYield struct {
	Instance pgapi.ScopeService
	Group    string
}

var getMembersYieldPool = sync.Pool{New: func() any { return &GetMembersYield{} }}

func AcquireGetMembersYield(instance pgapi.ScopeService, group string) *GetMembersYield {
	y := getMembersYieldPool.Get().(*GetMembersYield)
	y.Instance = instance
	y.Group = group
	return y
}

func ReleaseGetMembersYield(y *GetMembersYield) {
	y.Instance = nil
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
	cmd.Instance = y.Instance
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
	Instance pgapi.ScopeService
	Group    string
}

var getLocalMembersYieldPool = sync.Pool{New: func() any { return &GetLocalMembersYield{} }}

func AcquireGetLocalMembersYield(instance pgapi.ScopeService, group string) *GetLocalMembersYield {
	y := getLocalMembersYieldPool.Get().(*GetLocalMembersYield)
	y.Instance = instance
	y.Group = group
	return y
}

func ReleaseGetLocalMembersYield(y *GetLocalMembersYield) {
	y.Instance = nil
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
	cmd.Instance = y.Instance
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

type WhichGroupsYield struct {
	Instance pgapi.ScopeService
}

var whichGroupsYieldPool = sync.Pool{New: func() any { return &WhichGroupsYield{} }}

func AcquireWhichGroupsYield(instance pgapi.ScopeService) *WhichGroupsYield {
	y := whichGroupsYieldPool.Get().(*WhichGroupsYield)
	y.Instance = instance
	return y
}

func ReleaseWhichGroupsYield(y *WhichGroupsYield) {
	y.Instance = nil
	whichGroupsYieldPool.Put(y)
}

func (y *WhichGroupsYield) String() string       { return "<pg_which_groups_yield>" }
func (y *WhichGroupsYield) Type() lua.LValueType { return lua.LTUserData }
func (y *WhichGroupsYield) CmdID() dispatcher.CommandID {
	return pgapi.WhichGroups
}

func (y *WhichGroupsYield) ToCommand() dispatcher.Command {
	cmd := pgapi.AcquireWhichGroupsCmd()
	cmd.Instance = y.Instance
	return cmd
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

	groups := result.Groups

	tbl := lua.CreateTable(len(groups), 0)
	for i, g := range groups {
		tbl.RawSetInt(i+1, lua.LString(g))
	}
	return []lua.LValue{tbl, lua.LNil}
}

// --- WhichLocalGroupsYield ---

type WhichLocalGroupsYield struct {
	Instance pgapi.ScopeService
}

var whichLocalGroupsYieldPool = sync.Pool{New: func() any { return &WhichLocalGroupsYield{} }}

func AcquireWhichLocalGroupsYield(instance pgapi.ScopeService) *WhichLocalGroupsYield {
	y := whichLocalGroupsYieldPool.Get().(*WhichLocalGroupsYield)
	y.Instance = instance
	return y
}

func ReleaseWhichLocalGroupsYield(y *WhichLocalGroupsYield) {
	y.Instance = nil
	whichLocalGroupsYieldPool.Put(y)
}

func (y *WhichLocalGroupsYield) String() string       { return "<pg_which_local_groups_yield>" }
func (y *WhichLocalGroupsYield) Type() lua.LValueType { return lua.LTUserData }
func (y *WhichLocalGroupsYield) CmdID() dispatcher.CommandID {
	return pgapi.WhichLocalGroups
}

func (y *WhichLocalGroupsYield) ToCommand() dispatcher.Command {
	cmd := pgapi.AcquireWhichLocalGroupsCmd()
	cmd.Instance = y.Instance
	return cmd
}

func (y *WhichLocalGroupsYield) Release() { ReleaseWhichLocalGroupsYield(y) }

func (y *WhichLocalGroupsYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "pg which_local_groups")}
	}
	result, ok := data.(pgapi.WhichLocalGroupsResult)
	if !ok {
		return []lua.LValue{lua.CreateTable(0, 0), lua.LNil}
	}

	groups := result.Groups

	tbl := lua.CreateTable(len(groups), 0)
	for i, g := range groups {
		tbl.RawSetInt(i+1, lua.LString(g))
	}
	return []lua.LValue{tbl, lua.LNil}
}

// --- BroadcastYield ---

type BroadcastYield struct {
	Instance pgapi.ScopeService
	From     pid.PID
	Group    string
	Topic    string
	Payloads payload.Payloads
}

var broadcastYieldPool = sync.Pool{New: func() any { return &BroadcastYield{} }}

func AcquireBroadcastYield(instance pgapi.ScopeService, from pid.PID, group, topic string, payloads payload.Payloads) *BroadcastYield {
	y := broadcastYieldPool.Get().(*BroadcastYield)
	y.Instance = instance
	y.From = from
	y.Group = group
	y.Topic = topic
	y.Payloads = payloads
	return y
}

func ReleaseBroadcastYield(y *BroadcastYield) {
	y.Instance = nil
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
	cmd.Instance = y.Instance
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
	Instance pgapi.ScopeService
	From     pid.PID
	Group    string
	Topic    string
	Payloads payload.Payloads
}

var broadcastLocalYieldPool = sync.Pool{New: func() any { return &BroadcastLocalYield{} }}

func AcquireBroadcastLocalYield(instance pgapi.ScopeService, from pid.PID, group, topic string, payloads payload.Payloads) *BroadcastLocalYield {
	y := broadcastLocalYieldPool.Get().(*BroadcastLocalYield)
	y.Instance = instance
	y.From = from
	y.Group = group
	y.Topic = topic
	y.Payloads = payloads
	return y
}

func ReleaseBroadcastLocalYield(y *BroadcastLocalYield) {
	y.Instance = nil
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
	cmd.Instance = y.Instance
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

// --- EventsYield ---
// Subscribes a process to pg membership change events via the event bus.

const pgSubscriptionTypeName = "pg.Subscription"

// Subscription wraps a channel and unsubscribe function for pg events.
type Subscription struct {
	channelUD   *lua.LUserData
	channel     *engine.Channel
	unsubscribe func()
	closed      bool
	mu          sync.Mutex
}

func init() {
	value.RegisterTypeMethods(nil, pgSubscriptionTypeName,
		map[string]lua.LGoFunc{"__tostring": pgSubscriptionToString},
		map[string]lua.LGoFunc{
			"channel": pgSubscriptionChannel,
			"close":   pgSubscriptionClose,
		})
}

func checkPGSubscription(l *lua.LState) *Subscription {
	ud := l.CheckUserData(1)
	if sub, ok := ud.Value.(*Subscription); ok {
		return sub
	}
	l.ArgError(1, "pg.Subscription expected")
	return nil
}

func pgSubscriptionToString(l *lua.LState) int {
	l.Push(lua.LString("pg.Subscription{}"))
	return 1
}

func pgSubscriptionChannel(l *lua.LState) int {
	sub := checkPGSubscription(l)
	if sub == nil {
		return 0
	}
	l.Push(sub.channelUD)
	return 1
}

func pgSubscriptionClose(l *lua.LState) int {
	sub := checkPGSubscription(l)
	if sub == nil {
		return 0
	}

	// Check for optional options table: sub:close({flush=true})
	flush := false
	if l.GetTop() >= 2 {
		if opts, ok := l.Get(2).(*lua.LTable); ok {
			if v := opts.RawGetString("flush"); v == lua.LTrue {
				flush = true
			}
		}
	}

	sub.mu.Lock()
	defer sub.mu.Unlock()

	if sub.closed {
		l.Push(lua.LTrue)
		return 1
	}

	sub.closed = true
	// Unsubscribe first (synchronous — blocks until the event loop
	// processes the removal, guaranteeing no new events will be emitted).
	if sub.unsubscribe != nil {
		sub.unsubscribe()
		sub.unsubscribe = nil
	}
	// Then drain any in-flight messages that arrived before removal.
	if sub.channel != nil {
		if flush {
			sub.channel.Drain()
		}
		sub.channel.Close(nil)
	}

	l.Push(lua.LTrue)
	return 1
}

type EventsYield struct {
	Instance pgapi.ScopeService
	Channel  *engine.Channel
	PID      pid.PID
	Topic    string
}

var eventsYieldPool = sync.Pool{New: func() any { return &EventsYield{} }}

func AcquireEventsYield(instance pgapi.ScopeService, ch *engine.Channel, p pid.PID, topic string) *EventsYield {
	y := eventsYieldPool.Get().(*EventsYield)
	y.Instance = instance
	y.Channel = ch
	y.PID = p
	y.Topic = topic
	return y
}

func ReleaseEventsYield(y *EventsYield) {
	y.Instance = nil
	y.Channel = nil
	y.PID = pid.PID{}
	y.Topic = ""
	eventsYieldPool.Put(y)
}

func (y *EventsYield) String() string       { return "<pg_events_yield>" }
func (y *EventsYield) Type() lua.LValueType { return lua.LTUserData }

func (y *EventsYield) CmdID() dispatcher.CommandID {
	return pgapi.Events
}

func (y *EventsYield) ToCommand() dispatcher.Command {
	cmd := pgapi.AcquireEventsCmd()
	cmd.Instance = y.Instance
	cmd.PID = y.PID
	cmd.Topic = y.Topic
	return cmd
}

func (y *EventsYield) Release() { ReleaseEventsYield(y) }

// HandleResult sets up the topic subscription and returns (subscription, groups_snapshot).
func (y *EventsYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LNil, lua.WrapErrorWithLua(l, err, "pg events")}
	}

	result, ok := data.(pgapi.EventsResult)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LNil, lua.WrapErrorWithLua(l, errUnexpectedResult, "pg events")}
	}

	// Create channel userdata
	channelUD := engine.PushChannel(l, y.Channel)
	l.Pop(1) // Remove from stack since we return via slice

	// Subscribe externally-owned channel to topic if we're in a process context
	proc := engine.GetProcess(l)
	if proc != nil {
		if err := proc.SubscribeExisting(y.Topic, y.Channel); err != nil {
			return []lua.LValue{lua.LNil, lua.LNil, lua.WrapErrorWithLua(l, err, "pg events")}
		}
	}

	// Create subscription with channel and unsubscribe function
	sub := &Subscription{
		channelUD: channelUD,
		channel:   y.Channel,
	}

	if result.Unsubscribe != nil {
		sub.unsubscribe = result.Unsubscribe

		// Register cleanup to unsubscribe from dispatcher when frame is released
		ctx := l.Context()
		if ctx != nil {
			if store := resource.GetStore(ctx); store != nil {
				store.AddCleanup(func() error {
					sub.mu.Lock()
					defer sub.mu.Unlock()
					if !sub.closed {
						sub.closed = true
						if sub.unsubscribe != nil {
							sub.unsubscribe()
							sub.unsubscribe = nil
						}
						sub.channel.Close(nil)
					}
					return nil
				})
			}
		}
	}

	// Wrap in Subscription userdata
	subUD := value.PushTypedUserData(l, sub, pgSubscriptionTypeName)
	l.Pop(1) // Remove from stack since we return via slice

	// Build groups snapshot table: {group_name = {pid1, pid2, ...}, ...}
	groupsTbl := lua.CreateTable(0, len(result.Groups))
	for group, members := range result.Groups {
		membersTbl := lua.CreateTable(len(members), 0)
		for i, p := range members {
			membersTbl.RawSetInt(i+1, lua.LString(p.String()))
		}
		groupsTbl.RawSetString(group, membersTbl)
	}

	return []lua.LValue{subUD, groupsTbl, lua.LNil}
}

// --- MonitorYield ---
// Atomically subscribes to a group's membership events and returns current members.

type MonitorYield struct {
	Instance pgapi.ScopeService
	Channel  *engine.Channel
	Group    string
	PID      pid.PID
	Topic    string
}

var monitorYieldPool = sync.Pool{New: func() any { return &MonitorYield{} }}

func AcquireMonitorYield(instance pgapi.ScopeService, ch *engine.Channel, group string, p pid.PID, topic string) *MonitorYield {
	y := monitorYieldPool.Get().(*MonitorYield)
	y.Instance = instance
	y.Channel = ch
	y.Group = group
	y.PID = p
	y.Topic = topic
	return y
}

func ReleaseMonitorYield(y *MonitorYield) {
	y.Instance = nil
	y.Channel = nil
	y.Group = ""
	y.PID = pid.PID{}
	y.Topic = ""
	monitorYieldPool.Put(y)
}

func (y *MonitorYield) String() string       { return "<pg_monitor_yield>" }
func (y *MonitorYield) Type() lua.LValueType { return lua.LTUserData }

func (y *MonitorYield) CmdID() dispatcher.CommandID {
	return pgapi.Monitor
}

func (y *MonitorYield) ToCommand() dispatcher.Command {
	cmd := pgapi.AcquireMonitorCmd()
	cmd.Instance = y.Instance
	cmd.Group = y.Group
	cmd.PID = y.PID
	cmd.Topic = y.Topic
	return cmd
}

func (y *MonitorYield) Release() { ReleaseMonitorYield(y) }

// HandleResult sets up the topic subscription and returns (subscription, members_table).
func (y *MonitorYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LNil, lua.WrapErrorWithLua(l, err, "pg monitor")}
	}

	result, ok := data.(pgapi.MonitorResult)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LNil, lua.WrapErrorWithLua(l, errUnexpectedResult, "pg monitor")}
	}

	// Create channel userdata
	channelUD := engine.PushChannel(l, y.Channel)
	l.Pop(1)

	// Subscribe externally-owned channel to topic
	proc := engine.GetProcess(l)
	if proc != nil {
		if err := proc.SubscribeExisting(y.Topic, y.Channel); err != nil {
			return []lua.LValue{lua.LNil, lua.LNil, lua.WrapErrorWithLua(l, err, "pg monitor")}
		}
	}

	// Create subscription with channel and unsubscribe function
	sub := &Subscription{
		channelUD: channelUD,
		channel:   y.Channel,
	}

	if result.Unsubscribe != nil {
		sub.unsubscribe = result.Unsubscribe

		ctx := l.Context()
		if ctx != nil {
			if store := resource.GetStore(ctx); store != nil {
				store.AddCleanup(func() error {
					sub.mu.Lock()
					defer sub.mu.Unlock()
					if !sub.closed {
						sub.closed = true
						if sub.unsubscribe != nil {
							sub.unsubscribe()
							sub.unsubscribe = nil
						}
						sub.channel.Close(nil)
					}
					return nil
				})
			}
		}
	}

	// Wrap subscription in userdata
	subUD := value.PushTypedUserData(l, sub, pgSubscriptionTypeName)
	l.Pop(1)

	// Build members table
	members := result.Members
	tbl := lua.CreateTable(len(members), 0)
	for i, p := range members {
		tbl.RawSetInt(i+1, lua.LString(p.String()))
	}

	return []lua.LValue{subUD, tbl, lua.LNil}
}
