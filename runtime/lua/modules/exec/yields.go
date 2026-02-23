// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	execapi "github.com/wippyai/runtime/api/service/exec"
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
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "wait process").WithKind(lua.Internal).WithRetryable(false)}
	}
	resp, ok := data.(execapi.ProcessWaitResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.Internal).WithRetryable(false)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, resp.Error, "process exit").WithKind(lua.Internal).WithRetryable(false)}
	}
	return []lua.LValue{lua.LNumber(resp.ExitCode), lua.LNil}
}
