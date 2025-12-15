package workflow

import "github.com/wippyai/runtime/api/dispatcher"

func init() {
	dispatcher.MustRegisterCommands("workflow", SideEffect)
}

// SideEffect is the command ID for workflow side effect operations.
const SideEffect dispatcher.CommandID = 260

// SideEffectCmd carries a closure to be executed by the workflow dispatcher.
// The dispatcher will execute the function and record the result for replay.
type SideEffectCmd struct {
	Fn func() (any, error)
}

// CmdID implements dispatcher.Command.
func (c *SideEffectCmd) CmdID() dispatcher.CommandID { return SideEffect }

// Result from side effect execution.
type Result struct {
	Value any
	Error error
}
