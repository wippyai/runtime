// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
)

func init() {
	dispatcher.MustRegisterCommands("exec", ProcessWait, TerminalReady)
}

// ProcessWait is a command ID for exec operations.
const (
	ProcessWait   dispatcher.CommandID = 150 // Wait for process to complete
	TerminalReady dispatcher.CommandID = 151 // Wait for terminal process startup
)

// TerminalReadyCmd waits without blocking the Lua scheduler until a terminal
// proxy has started its child and validated the initial PTY setup.
type TerminalReadyCmd struct {
	Ready <-chan error
}

func (c *TerminalReadyCmd) CmdID() dispatcher.CommandID { return TerminalReady }

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
	Error    error
	ExitCode int
}
