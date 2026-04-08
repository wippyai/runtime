// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
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

	sub.mu.Lock()
	defer sub.mu.Unlock()

	if sub.closed {
		l.Push(lua.LTrue)
		return 1
	}

	sub.closed = true
	if sub.unsubscribe != nil {
		sub.unsubscribe()
		sub.unsubscribe = nil
	}
	if sub.channel != nil {
		sub.channel.Close(nil)
	}

	l.Push(lua.LTrue)
	return 1
}

type EventsYield struct {
	Channel *engine.Channel
	PID     pid.PID
	Topic   string
}

var eventsYieldPool = sync.Pool{New: func() any { return &EventsYield{} }}

func AcquireEventsYield(ch *engine.Channel, p pid.PID, topic string) *EventsYield {
	y := eventsYieldPool.Get().(*EventsYield)
	y.Channel = ch
	y.PID = p
	y.Topic = topic
	return y
}

func ReleaseEventsYield(y *EventsYield) {
	y.Channel = nil
	y.PID = pid.PID{}
	y.Topic = ""
	eventsYieldPool.Put(y)
}

func (y *EventsYield) String() string       { return "<pg_events_yield>" }
func (y *EventsYield) Type() lua.LValueType { return lua.LTUserData }

func (y *EventsYield) CmdID() dispatcher.CommandID {
	return event.Subscribe
}

func (y *EventsYield) ToCommand() dispatcher.Command {
	return event.SubscribeCmd{
		System: pgapi.EventSystem,
		Kind:   "*",
		Topic:  y.Topic,
		PID:    y.PID,
	}
}

func (y *EventsYield) Release() { ReleaseEventsYield(y) }

// HandleResult sets up the topic subscription and returns a Subscription object.
func (y *EventsYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "pg events")}
	}

	// Create channel userdata
	channelUD := engine.PushChannel(l, y.Channel)
	l.Pop(1) // Remove from stack since we return via slice

	// Subscribe externally-owned channel to topic if we're in a process context
	proc := engine.GetProcess(l)
	if proc != nil {
		if err := proc.SubscribeExisting(y.Topic, y.Channel); err != nil {
			return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "pg events")}
		}
	}

	// Create subscription with channel and unsubscribe function
	sub := &Subscription{
		channelUD: channelUD,
		channel:   y.Channel,
	}

	// Store unsubscribe function from dispatcher
	if eventSub, ok := data.(event.Subscription); ok && eventSub.Unsubscribe != nil {
		sub.unsubscribe = eventSub.Unsubscribe

		// Register cleanup to unsubscribe from dispatcher when frame is released
		ctx := l.Context()
		if ctx != nil {
			if store := resource.GetStore(ctx); store != nil {
				store.AddCleanup(func() error {
					sub.mu.Lock()
					defer sub.mu.Unlock()
					if !sub.closed && sub.unsubscribe != nil {
						sub.unsubscribe()
						sub.unsubscribe = nil
					}
					return nil
				})
			}
		}
	}

	// Wrap in Subscription userdata
	subUD := value.PushTypedUserData(l, sub, pgSubscriptionTypeName)
	l.Pop(1) // Remove from stack since we return via slice

	return []lua.LValue{subUD, lua.LNil}
}
