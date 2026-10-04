// SPDX-License-Identifier: MPL-2.0

package engine

import (
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/inspect"
	"github.com/wippyai/runtime/runtime/lua/engine/loadlib"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

// ChannelTypeName is the Lua metatable type name for channels.
const ChannelTypeName = "channel"

// SelectCase wraps a channel case for select operations.
type SelectCase struct {
	Value   lua.LValue
	Channel *Channel
	Kind    ChannelOpKind
}

func (s *SelectCase) String() string       { return "<select_case>" }
func (s *SelectCase) Type() lua.LValueType { return lua.LTUserData }

// checkChannel extracts a Channel from userdata with proper error handling.
func checkChannel(l *lua.LState, idx int) *Channel {
	ud := l.CheckUserData(idx)
	if ch, ok := ud.Value.(*Channel); ok {
		return ch
	}
	l.ArgError(idx, "channel expected")
	return nil
}

// channelNewFunc creates a new channel with optional buffer size.
func channelNewFunc(l *lua.LState) int {
	bufSize := l.OptInt(1, 0)
	ch := NewChannel(bufSize)
	PushChannel(l, ch)
	return 1
}

// PushChannel creates a channel userdata, sets up the metatable,
// links the channel value reference, pushes to stack, and returns the userdata.
// If the channel already has a cached userdata value, reuses it for identity stability.
func PushChannel(l *lua.LState, ch *Channel) *lua.LUserData {
	if cached := ch.Value(); cached != nil {
		if ud, ok := cached.(*lua.LUserData); ok {
			l.Push(ud)
			return ud
		}
	}

	ud := l.NewUserData()
	ud.Value = ch
	ud.Metatable = value.GetTypeMetatable(nil, ChannelTypeName)
	ch.SetValue(ud)
	l.Push(ud)
	return ud
}

// channelSelectFunc implements channel.select{cases...}.
func channelSelectFunc(l *lua.LState) int {
	casesTable := l.CheckTable(1)
	hasDefault := l.OptBool(2, false)
	caseCount := casesTable.Len()

	selectOp := &SelectOp{
		Task:       l,
		Cases:      make([]*ChannelOp, 0, caseCount),
		HasDefault: hasDefault,
	}
	// Allocate array-case operations in small invocation-owned batches. This
	// reduces objects without a large sparse allocation or changing operation
	// identity when callers share descriptors between concurrent selects.
	const operationBatchSize = 8
	var caseOps []ChannelOp

	appendCase := func(value lua.LValue) {
		sc := checkSelectCaseValue(value)
		if sc != nil {
			if len(caseOps) == 0 {
				if n := min(operationBatchSize, caseCount-len(selectOp.Cases)); n > 0 {
					caseOps = make([]ChannelOp, n)
				}
			}
			var op *ChannelOp
			if len(caseOps) > 0 {
				op, caseOps = &caseOps[0], caseOps[1:]
			} else {
				// Sparse/named cases can exceed the array-length estimate.
				// Allocate those separately so existing pointers stay stable.
				op = &ChannelOp{}
			}
			*op = ChannelOp{
				Kind:     sc.Kind,
				Channel:  sc.Channel,
				Value:    sc.Value,
				Task:     l,
				SelectOp: selectOp,
			}
			selectOp.Cases = append(selectOp.Cases, op)
		}
	}
	if len(casesTable.Strdict) == 0 && len(casesTable.Dict) == 0 {
		// Pure arrays have no "default" key and select ignores their indices.
		// As in the value/JSON converters, walk the exported array directly to
		// avoid ForEach boxing one unused numeric key per case.
		for _, v := range casesTable.Array {
			appendCase(v)
		}
	} else {
		// Preserve the general iterator's order and keyed/default semantics.
		casesTable.ForEach(func(key, value lua.LValue) {
			if key.Type() == lua.LTString && key.String() == "default" {
				if v, ok := value.(lua.LBool); ok && bool(v) {
					selectOp.HasDefault = true
				}
				return
			}
			appendCase(value)
		})
	}

	for _, caseOp := range selectOp.Cases {
		var canExecute bool
		if caseOp.Kind == SendOp {
			canExecute = caseOp.Channel.CanSend()
		} else {
			canExecute = caseOp.Channel.CanReceive()
		}

		if canExecute {
			var result *ChannelResult
			if caseOp.Kind == SendOp {
				result = caseOp.Channel.Send(l, caseOp.Value, selectOp)
			} else {
				result = caseOp.Channel.Receive(l, selectOp)
			}

			updates := result.GetUpdates()
			if len(updates) > 0 {
				res := updates[0].GetResult()
				if len(res) > 0 {
					// If there are other tasks to wake, yield so processChannelYields handles them
					if len(updates) > 1 || result.Yields {
						l.Push(result)
						return -1
					}
					l.Push(res[0])
					ReleaseResult(result)
					return 1
				}
			}
			ReleaseResult(result)
		}
	}

	if selectOp.HasDefault {
		result := l.CreateTable(0, 2)
		result.RawSetString("default", lua.LTrue)
		result.RawSetString("ok", lua.LTrue)
		l.Push(result)
		return 1
	}

	nNext := acquireResult()
	nNext.Yields = true

	for _, caseOp := range selectOp.Cases {
		var m *ChannelResult
		if caseOp.Kind == SendOp {
			m = caseOp.Channel.Send(l, caseOp.Value, selectOp)
		} else {
			m = caseOp.Channel.Receive(l, selectOp)
		}
		nNext.Block = append(nNext.Block, m.Block...)
		nNext.Release = append(nNext.Release, m.Release...)
		ReleaseResult(m)
	}

	l.Push(nNext)
	return -1
}

func checkSelectCaseValue(v lua.LValue) *SelectCase {
	ud, ok := v.(*lua.LUserData)
	if !ok {
		return nil
	}
	sc, ok := ud.Value.(*SelectCase)
	if !ok {
		return nil
	}
	return sc
}

// channelMethods defines all channel instance methods.
var channelMethods = map[string]lua.LGoFunc{
	"send":         channelSend,
	"receive":      channelReceive,
	"close":        channelClose,
	"case_send":    channelCaseSend,
	"case_receive": channelCaseReceive,
}

func channelSend(l *lua.LState) int {
	ch := checkChannel(l, 1)
	if ch == nil {
		return 0
	}
	val := l.Get(2)

	result := ch.Send(l, val, nil)
	if result.Yields {
		l.Push(result)
		return -1
	}
	defer ReleaseResult(result)
	updates := result.GetUpdates()
	if len(updates) > 0 {
		if updates[0].Error != nil {
			l.RaiseError("%s", updates[0].Error.Error())
			return 0
		}
		res := updates[0].GetResult()
		for _, v := range res {
			l.Push(v)
		}
		return len(res)
	}
	l.Push(lua.LTrue)
	return 1
}

func channelReceive(l *lua.LState) int {
	ch := checkChannel(l, 1)
	if ch == nil {
		return 0
	}

	result := ch.Receive(l, nil)
	if result.Yields {
		l.Push(result)
		return -1
	}
	defer ReleaseResult(result)
	updates := result.GetUpdates()
	if len(updates) > 0 {
		res := updates[0].GetResult()
		if len(res) > 0 {
			for _, v := range res {
				l.Push(v)
			}
			return len(res)
		}
	}
	l.Push(lua.LNil)
	l.Push(lua.LFalse)
	return 2
}

func channelClose(l *lua.LState) int {
	ch := checkChannel(l, 1)
	if ch == nil {
		return 0
	}

	result := ch.Close(l)
	if result != nil && result.Yields {
		l.Push(result)
		return -1
	}
	ReleaseResult(result)
	return 0
}

func channelCaseSend(l *lua.LState) int {
	ch := checkChannel(l, 1)
	if ch == nil {
		return 0
	}
	val := l.Get(2)

	sc := &SelectCase{
		Kind:    SendOp,
		Channel: ch,
		Value:   val,
	}
	caseUd := l.NewUserData()
	caseUd.Value = sc
	l.Push(caseUd)
	return 1
}

func channelCaseReceive(l *lua.LState) int {
	ch := checkChannel(l, 1)
	if ch == nil {
		return 0
	}

	sc := &SelectCase{
		Kind:    ReceiveOp,
		Channel: ch,
	}
	caseUd := l.NewUserData()
	caseUd.Value = sc
	l.Push(caseUd)
	return 1
}

// subscribeFunc subscribes a channel to a topic.
func subscribeFunc(l *lua.LState) int {
	topic := l.CheckString(1)
	ch := checkChannel(l, 2)
	if ch == nil {
		return 0
	}

	req := &SubscribeRequest{Topic: topic, ExistingChannel: ch}
	l.Push(req)
	return -1
}

// unsubscribeFunc unsubscribes a channel.
func unsubscribeFunc(l *lua.LState) int {
	ch := checkChannel(l, 1)
	if ch == nil {
		return 0
	}

	req := &UnsubscribeRequest{Channel: ch}
	l.Push(req)
	return -1
}

// OpenRestrictedPackage returns the restricted package loader that only supports preload.
func OpenRestrictedPackage(l *lua.LState) int {
	return loadlib.OpenRestrictedPackage(l)
}

// GetStackTrace captures a complete stack trace from a Lua state.
func GetStackTrace(l *lua.LState) *inspect.StackTrace {
	return inspect.GetStackTrace(l)
}

// GetStackFrame captures information about a single stack frame at given level.
func GetStackFrame(l *lua.LState, level int) (inspect.StackFrame, bool) {
	return inspect.GetStackFrame(l, level)
}

// GetCallerLine returns the line number of the caller at the given stack level.
func GetCallerLine(l *lua.LState, level int) (int, bool) {
	return inspect.GetCallerLine(l, level)
}
