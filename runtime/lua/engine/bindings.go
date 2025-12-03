package engine

import (
	"fmt"
	"strings"
	"sync"

	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/lua/engine/loadlib"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/modules/payload"
	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/inspect"
	"go.uber.org/zap"
)

const channelTypeName = "channel"

var channelMetatableOnce sync.Once

// SelectCase wraps a channel case for select operations.
type SelectCase struct {
	Kind    ChannelOpKind
	Channel *Channel
	Value   lua.LValue
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

// checkSelectCase extracts a SelectCase from userdata.
func checkSelectCase(l *lua.LState, idx int) *SelectCase {
	ud, ok := l.Get(idx).(*lua.LUserData)
	if !ok {
		return nil
	}
	sc, ok := ud.Value.(*SelectCase)
	if !ok {
		return nil
	}
	return sc
}

var (
	channelModuleTable *lua.LTable
	channelInitOnce    sync.Once
)

func getChannelModuleTable() *lua.LTable {
	channelInitOnce.Do(func() {
		channelModuleTable = &lua.LTable{}
		channelModuleTable.RawSetString("new", lua.LGoFunc(channelNewFunc))
		channelModuleTable.RawSetString("select", lua.LGoFunc(channelSelectFunc))
		channelModuleTable.Immutable = true
	})
	return channelModuleTable
}

// channelNewFunc creates a new channel with optional buffer size.
func channelNewFunc(l *lua.LState) int {
	bufSize := l.OptInt(1, 0)
	ch := NewChannel(bufSize)

	ud := l.NewUserData()
	ud.Value = ch
	ud.Metatable = value.GetTypeMetatable(nil, channelTypeName)
	l.Push(ud)
	return 1
}

// channelSelectFunc selects over multiple channel operations.
func channelSelectFunc(l *lua.LState) int {
	nargs := l.GetTop()
	if nargs == 0 {
		l.RaiseError("select requires at least one case")
		return 0
	}

	selectOp := &SelectOp{
		Task:  l,
		Cases: make([]*ChannelOp, 0, nargs),
	}

	for i := 1; i <= nargs; i++ {
		arg := l.Get(i)

		if arg == lua.LNil || arg == lua.LFalse {
			selectOp.HasDefault = true
			continue
		}

		sc := checkSelectCase(l, i)
		if sc == nil {
			l.RaiseError("select case %d: expected case_send/case_receive result", i)
			return 0
		}

		selectOp.Cases = append(selectOp.Cases, &ChannelOp{
			Kind:     sc.Kind,
			Channel:  sc.Channel,
			Value:    sc.Value,
			Task:     l,
			SelectOp: selectOp,
		})
	}

	for idx, caseOp := range selectOp.Cases {
		var result *ChannelResult
		if caseOp.Kind == SendOp {
			result = caseOp.Channel.Send(l, caseOp.Value, selectOp)
		} else {
			result = caseOp.Channel.Receive(l, selectOp)
		}

		updates := result.GetUpdates()
		if !result.Yields || len(updates) > 0 {
			l.Push(lua.LNumber(idx + 1))
			if caseOp.Kind == ReceiveOp && len(updates) > 0 {
				res := updates[0].GetResult()
				for _, v := range res {
					l.Push(v)
				}
				return 1 + len(res)
			}
			return 1
		}
	}

	if selectOp.HasDefault {
		l.Push(lua.LNumber(0))
		return 1
	}

	result := &ChannelResult{
		Yields: true,
		Block:  make([]*Channel, 0, len(selectOp.Cases)),
	}
	for _, c := range selectOp.Cases {
		result.Block = append(result.Block, c.Channel)
	}
	l.Push(result)
	return -1
}

// channelMethods defines all channel instance methods using package-level functions.
var channelMethods = map[string]lua.LGFunction{
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
	updates := result.GetUpdates()
	if len(updates) > 0 && updates[0].Error != nil {
		l.RaiseError("%s", updates[0].Error.Error())
		return 0
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

// registerChannelMetatable registers the shared channel metatable once.
func registerChannelMetatable() {
	channelMetatableOnce.Do(func() {
		value.RegisterTypeMethods(nil, channelTypeName, nil, channelMethods)
	})
}

// BindChannelFunctions binds channel.new and channel methods to Lua.
func BindChannelFunctions(l *lua.LState) {
	registerChannelMetatable()
	l.SetGlobal("channel", getChannelModuleTable())
}

// subscribeFunc subscribes a channel to a topic.
func subscribeFunc(l *lua.LState) int {
	topic := l.CheckString(1)
	ch := checkChannel(l, 2)
	if ch == nil {
		return 0
	}

	req := &SubscribeRequest{Topic: topic, Channel: ch}
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

// BindSubscribeFunctions binds subscribe/unsubscribe functions to Lua.
func BindSubscribeFunctions(l *lua.LState) {
	l.SetGlobal("subscribe", lua.LGoFunc(subscribeFunc))
	l.SetGlobal("unsubscribe", lua.LGoFunc(unsubscribeFunc))
}

// BindErrorsModule registers the errors module from go-lua.
func BindErrorsModule(l *lua.LState) {
	lua.OpenErrors(l)
}

// OpenRestrictedPackage returns the restricted package loader that only supports preload.
// Use this instead of lua.OpenPackage for sandboxed environments.
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

// BindPrint binds a custom print function that logs via the context logger.
// Falls back to fmt.Print if no logger is available.
func BindPrint(l *lua.LState) {
	l.SetGlobal("print", lua.LGoFunc(printFunc))
}

// printFunc is the implementation of the custom print function.
func printFunc(l *lua.LState) int {
	log := logs.GetLogger(l.Context())

	parts := make([]string, l.GetTop())
	for i := 1; i <= l.GetTop(); i++ {
		parts[i-1] = l.ToString(i)
	}
	msg := strings.Join(parts, " ")

	if log == nil {
		fmt.Print(msg)
		return 0
	}

	fields := make([]zap.Field, 0, 2)

	if pid, ok := runtime.GetFramePID(l.Context()); ok {
		fields = append(fields, zap.String("pid", pid.String()))
	}

	if id, ok := runtime.GetFrameID(l.Context()); ok {
		if line, ok := inspect.GetCallerLine(l, 1); ok {
			location := fmt.Sprintf("%s:%d", id.String(), line)
			fields = append(fields, zap.String("location", location))
		}
	}

	log.Info(msg, fields...)
	return 0
}

// BindPayloadModule registers the payload module.
func BindPayloadModule(l *lua.LState) {
	payload.Module.Load(l)
}

// coreBinders is the shared slice of stateless binders.
var coreBinders = []ModuleBinder{
	BindErrorsModule,
	BindPayloadModule,
	BindPrint,
	BindChannelFunctions,
	BindSubscribeFunctions,
}

// CoreBinders returns the base set of module binders shared by all components.
// These are stateless binders that don't require Process reference.
// Returns a shared slice - do not modify.
func CoreBinders() []ModuleBinder {
	return coreBinders
}
