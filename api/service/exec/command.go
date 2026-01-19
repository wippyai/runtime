package exec

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
)

func init() {
	dispatcher.MustRegisterCommands("exec", ProcessWait)
}

// ProcessWait is a command ID for exec operations.
const (
	ProcessWait dispatcher.CommandID = 150 // Wait for process to complete
)

// ProcessWaitCmd waits for a process to complete.
type ProcessWaitCmd struct {
	Process Process
}

var processWaitCmdPool = sync.Pool{New: func() any { return &ProcessWaitCmd{} }}

// AcquireProcessWaitCmd returns a pooled ProcessWaitCmd.
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
