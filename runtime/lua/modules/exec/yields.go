// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

// ProcessWaitYield wraps ProcessWaitCmd for Lua.
type ProcessWaitYield struct {
	*execapi.ProcessWaitCmd
}

var processWaitYieldPool = sync.Pool{New: func() any { return &ProcessWaitYield{} }}

func AcquireProcessWaitYield() *ProcessWaitYield {
	y := processWaitYieldPool.Get().(*ProcessWaitYield)
	y.ProcessWaitCmd = execapi.AcquireProcessWaitCmd()
	return y
}

func ReleaseProcessWaitYield(y *ProcessWaitYield) {
	if y.ProcessWaitCmd != nil {
		y.ProcessWaitCmd.Release()
		y.ProcessWaitCmd = nil
	}
	processWaitYieldPool.Put(y)
}

func (y *ProcessWaitYield) String() string                { return "<process_wait_yield>" }
func (y *ProcessWaitYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *ProcessWaitYield) CmdID() dispatcher.CommandID   { return execapi.ProcessWait }
func (y *ProcessWaitYield) ToCommand() dispatcher.Command { return y.ProcessWaitCmd }
func (y *ProcessWaitYield) Release()                      { ReleaseProcessWaitYield(y) }

// HandleResult converts the dispatcher response to Lua values.
func (y *ProcessWaitYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, wrapExecError(l, err, "wait process", lua.Internal)}
	}
	resp, ok := data.(execapi.ProcessWaitResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.Internal).WithRetryable(false)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, wrapExecError(l, resp.Error, "process exit", lua.Internal)}
	}
	return []lua.LValue{lua.LNumber(resp.ExitCode), lua.LNil}
}

// TerminalReadyYield resumes the Lua caller only after the proxy owns a
// started PTY process. The proxy remains responsible for completion and reap.
type TerminalReadyYield struct {
	Ready      <-chan error
	Session    *terminalSession
	Completion *terminalCompletion
}

func (y *TerminalReadyYield) String() string              { return "<terminal_ready_yield>" }
func (y *TerminalReadyYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *TerminalReadyYield) CmdID() dispatcher.CommandID { return execapi.TerminalReady }
func (y *TerminalReadyYield) ToCommand() dispatcher.Command {
	return &execapi.TerminalReadyCmd{Ready: y.Ready}
}
func (y *TerminalReadyYield) Release() {}

func (y *TerminalReadyYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	if err != nil {
		if y.Completion != nil {
			y.Completion.close()
		}
		return []lua.LValue{lua.LNil, wrapExecError(l, err, "start terminal process", lua.Internal)}
	}
	ud := value.PushTypedUserData(l, y.Session, terminalProcessTypeName)
	l.Pop(1)
	return []lua.LValue{ud, lua.LNil}
}
