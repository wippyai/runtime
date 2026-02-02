package process

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	lua "github.com/wippyai/go-lua"
)

// SendYield wraps SendCmd for Lua.
type SendYield struct {
	*process.SendCmd
}

var sendYieldPool = sync.Pool{New: func() any { return &SendYield{} }}

func AcquireSendYield() *SendYield {
	y := sendYieldPool.Get().(*SendYield)
	y.SendCmd = process.AcquireSendCmd()
	return y
}

func ReleaseSendYield(y *SendYield) {
	if y.SendCmd != nil {
		y.SendCmd.Release()
		y.SendCmd = nil
	}
	sendYieldPool.Put(y)
}

func (y *SendYield) String() string                { return "<process_send_yield>" }
func (y *SendYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *SendYield) ToCommand() dispatcher.Command { return y.SendCmd }
func (y *SendYield) CmdID() dispatcher.CommandID   { return process.Send }
func (y *SendYield) Release()                      { ReleaseSendYield(y) }

func (y *SendYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "send failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if data != nil {
		if result, ok := data.(process.SendResult); ok && result.Error != nil {
			luaErr := lua.WrapErrorWithLua(l, result.Error, "").
				WithKind(lua.Internal).
				WithRetryable(false)
			return []lua.LValue{lua.LNil, luaErr}
		}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// SpawnYield wraps SpawnCmd for Lua.
type SpawnYield struct {
	*process.SpawnCmd
}

var spawnYieldPool = sync.Pool{New: func() any { return &SpawnYield{} }}

func AcquireSpawnYield() *SpawnYield {
	y := spawnYieldPool.Get().(*SpawnYield)
	y.SpawnCmd = process.AcquireSpawnCmd()
	return y
}

func ReleaseSpawnYield(y *SpawnYield) {
	if y.SpawnCmd != nil {
		y.SpawnCmd.Release()
		y.SpawnCmd = nil
	}
	spawnYieldPool.Put(y)
}

func (y *SpawnYield) String() string                { return "<process_spawn_yield>" }
func (y *SpawnYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *SpawnYield) ToCommand() dispatcher.Command { return y.SpawnCmd }
func (y *SpawnYield) CmdID() dispatcher.CommandID   { return process.Spawn }
func (y *SpawnYield) Release()                      { ReleaseSpawnYield(y) }

func (y *SpawnYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "spawn failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if data == nil {
		luaErr := lua.NewLuaError(l, "no response received").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	resp, ok := data.(process.SpawnResult)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if resp.Error != nil {
		luaErr := lua.WrapErrorWithLua(l, resp.Error, "")
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lua.LString(resp.PID.String()), lua.LNil}
}

// TerminateYield wraps TerminateCmd for Lua.
type TerminateYield struct {
	*process.TerminateCmd
}

var terminateYieldPool = sync.Pool{New: func() any { return &TerminateYield{} }}

func AcquireTerminateYield() *TerminateYield {
	y := terminateYieldPool.Get().(*TerminateYield)
	y.TerminateCmd = process.AcquireTerminateCmd()
	return y
}

func ReleaseTerminateYield(y *TerminateYield) {
	if y.TerminateCmd != nil {
		y.TerminateCmd.Release()
		y.TerminateCmd = nil
	}
	terminateYieldPool.Put(y)
}

func (y *TerminateYield) String() string                { return "<process_terminate_yield>" }
func (y *TerminateYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *TerminateYield) ToCommand() dispatcher.Command { return y.TerminateCmd }
func (y *TerminateYield) CmdID() dispatcher.CommandID   { return process.Terminate }
func (y *TerminateYield) Release()                      { ReleaseTerminateYield(y) }

func (y *TerminateYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "terminate failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// CancelYield wraps CancelCmd for Lua.
type CancelYield struct {
	*process.CancelCmd
}

var cancelYieldPool = sync.Pool{New: func() any { return &CancelYield{} }}

func AcquireCancelYield() *CancelYield {
	y := cancelYieldPool.Get().(*CancelYield)
	y.CancelCmd = process.AcquireCancelCmd()
	return y
}

func ReleaseCancelYield(y *CancelYield) {
	if y.CancelCmd != nil {
		y.CancelCmd.Release()
		y.CancelCmd = nil
	}
	cancelYieldPool.Put(y)
}

func (y *CancelYield) String() string                { return "<process_cancel_yield>" }
func (y *CancelYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *CancelYield) ToCommand() dispatcher.Command { return y.CancelCmd }
func (y *CancelYield) CmdID() dispatcher.CommandID   { return process.Cancel }
func (y *CancelYield) Release()                      { ReleaseCancelYield(y) }

func (y *CancelYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "cancel failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// MonitorYield wraps MonitorCmd for Lua.
type MonitorYield struct {
	*process.MonitorCmd
}

var monitorYieldPool = sync.Pool{New: func() any { return &MonitorYield{} }}

func AcquireMonitorYield() *MonitorYield {
	y := monitorYieldPool.Get().(*MonitorYield)
	y.MonitorCmd = process.AcquireMonitorCmd()
	return y
}

func ReleaseMonitorYield(y *MonitorYield) {
	if y.MonitorCmd != nil {
		y.MonitorCmd.Release()
		y.MonitorCmd = nil
	}
	monitorYieldPool.Put(y)
}

func (y *MonitorYield) String() string                { return "<process_monitor_yield>" }
func (y *MonitorYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *MonitorYield) ToCommand() dispatcher.Command { return y.MonitorCmd }
func (y *MonitorYield) CmdID() dispatcher.CommandID   { return process.Monitor }
func (y *MonitorYield) Release()                      { ReleaseMonitorYield(y) }

func (y *MonitorYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "monitor failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// UnmonitorYield wraps UnmonitorCmd for Lua.
type UnmonitorYield struct {
	*process.UnmonitorCmd
}

var unmonitorYieldPool = sync.Pool{New: func() any { return &UnmonitorYield{} }}

func AcquireUnmonitorYield() *UnmonitorYield {
	y := unmonitorYieldPool.Get().(*UnmonitorYield)
	y.UnmonitorCmd = process.AcquireUnmonitorCmd()
	return y
}

func ReleaseUnmonitorYield(y *UnmonitorYield) {
	if y.UnmonitorCmd != nil {
		y.UnmonitorCmd.Release()
		y.UnmonitorCmd = nil
	}
	unmonitorYieldPool.Put(y)
}

func (y *UnmonitorYield) String() string                { return "<process_unmonitor_yield>" }
func (y *UnmonitorYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *UnmonitorYield) ToCommand() dispatcher.Command { return y.UnmonitorCmd }
func (y *UnmonitorYield) CmdID() dispatcher.CommandID   { return process.Unmonitor }
func (y *UnmonitorYield) Release()                      { ReleaseUnmonitorYield(y) }

func (y *UnmonitorYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "unmonitor failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// LinkYield wraps LinkCmd for Lua.
type LinkYield struct {
	*process.LinkCmd
}

var linkYieldPool = sync.Pool{New: func() any { return &LinkYield{} }}

func AcquireLinkYield() *LinkYield {
	y := linkYieldPool.Get().(*LinkYield)
	y.LinkCmd = process.AcquireLinkCmd()
	return y
}

func ReleaseLinkYield(y *LinkYield) {
	if y.LinkCmd != nil {
		y.LinkCmd.Release()
		y.LinkCmd = nil
	}
	linkYieldPool.Put(y)
}

func (y *LinkYield) String() string                { return "<process_link_yield>" }
func (y *LinkYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *LinkYield) ToCommand() dispatcher.Command { return y.LinkCmd }
func (y *LinkYield) CmdID() dispatcher.CommandID   { return process.Link }
func (y *LinkYield) Release()                      { ReleaseLinkYield(y) }

func (y *LinkYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "link failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// UnlinkYield wraps UnlinkCmd for Lua.
type UnlinkYield struct {
	*process.UnlinkCmd
}

var unlinkYieldPool = sync.Pool{New: func() any { return &UnlinkYield{} }}

func AcquireUnlinkYield() *UnlinkYield {
	y := unlinkYieldPool.Get().(*UnlinkYield)
	y.UnlinkCmd = process.AcquireUnlinkCmd()
	return y
}

func ReleaseUnlinkYield(y *UnlinkYield) {
	if y.UnlinkCmd != nil {
		y.UnlinkCmd.Release()
		y.UnlinkCmd = nil
	}
	unlinkYieldPool.Put(y)
}

func (y *UnlinkYield) String() string                { return "<process_unlink_yield>" }
func (y *UnlinkYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *UnlinkYield) ToCommand() dispatcher.Command { return y.UnlinkCmd }
func (y *UnlinkYield) CmdID() dispatcher.CommandID   { return process.Unlink }
func (y *UnlinkYield) Release()                      { ReleaseUnlinkYield(y) }

func (y *UnlinkYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "unlink failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// ExecYield wraps ExecCmd for Lua.
type ExecYield struct {
	*process.ExecCmd
}

var execYieldPool = sync.Pool{New: func() any { return &ExecYield{} }}

func AcquireExecYield() *ExecYield {
	y := execYieldPool.Get().(*ExecYield)
	y.ExecCmd = process.AcquireExecCmd()
	return y
}

func ReleaseExecYield(y *ExecYield) {
	if y.ExecCmd != nil {
		y.ExecCmd.Release()
		y.ExecCmd = nil
	}
	execYieldPool.Put(y)
}

func (y *ExecYield) String() string                { return "<process_exec_yield>" }
func (y *ExecYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *ExecYield) ToCommand() dispatcher.Command { return y.ExecCmd }
func (y *ExecYield) CmdID() dispatcher.CommandID   { return process.Exec }
func (y *ExecYield) Release()                      { ReleaseExecYield(y) }

func (y *ExecYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "exec failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	if data == nil {
		return []lua.LValue{lua.LNil, lua.LNil}
	}

	result, ok := data.(process.ExecResult)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid exec result type").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	if result.Result == nil {
		return []lua.LValue{lua.LNil, lua.LNil}
	}

	if result.Result.Error != nil {
		luaErr := lua.WrapErrorWithLua(l, result.Result.Error, "").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	// Convert result value to Lua value
	if result.Result.Value == nil {
		return []lua.LValue{lua.LNil, lua.LNil}
	}

	luaValue, err := luaconv.GoToLua(result.Result.Value.Data())
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "failed to convert result").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{luaValue, lua.LNil}
}
