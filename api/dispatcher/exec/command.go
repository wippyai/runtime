// Package execapi provides exec command types for the dispatcher system.
package execapi

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	apiexec "github.com/wippyai/runtime/api/service/exec"
)

func init() {
	dispatcher.MustRegisterCommands("exec", CmdProcessWait)
}

// Command IDs for exec operations.
// Range 210-219 is reserved for exec commands (func uses 200-203).
const (
	CmdProcessWait dispatcher.CommandID = 210 // Wait for process to complete
)

// ProcessWaitCmd waits for a process to complete.
type ProcessWaitCmd struct {
	Process apiexec.Process
}

var processWaitCmdPool = sync.Pool{New: func() any { return &ProcessWaitCmd{} }}

func AcquireProcessWaitCmd() *ProcessWaitCmd          { return processWaitCmdPool.Get().(*ProcessWaitCmd) }
func (c *ProcessWaitCmd) CmdID() dispatcher.CommandID { return CmdProcessWait }
func (c *ProcessWaitCmd) Release() {
	c.Process = nil
	processWaitCmdPool.Put(c)
}

// ProcessWaitResponse contains the result of waiting for a process.
type ProcessWaitResponse struct {
	ExitCode int
	Error    error
}
