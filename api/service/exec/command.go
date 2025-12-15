package exec

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
)

func init() {
	dispatcher.MustRegisterCommands("exec", ProcessWait)
}

// Command IDs for exec operations.
// Range 210-219 is reserved for exec commands (func uses 200-203).
const (
	ProcessWait dispatcher.CommandID = 210 // Wait for process to complete
)

// ProcessWaitCmd waits for a process to complete.
type ProcessWaitCmd struct {
	Process Process
}

var processWaitCmdPool = sync.Pool{New: func() any { return &ProcessWaitCmd{} }}

func AcquireProcessWaitCmd() *ProcessWaitCmd          { return processWaitCmdPool.Get().(*ProcessWaitCmd) }
func (c *ProcessWaitCmd) CmdID() dispatcher.CommandID { return ProcessWait }
func (c *ProcessWaitCmd) Release() {
	c.Process = nil
	processWaitCmdPool.Put(c)
}

// ProcessWaitResponse contains the result of waiting for a process.
type ProcessWaitResponse struct {
	ExitCode int
	Error    error
}
