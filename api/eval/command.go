// Package eval provides command types for dynamic Lua code evaluation.
// Commands yield to the dispatcher for compilation and execution of Lua code.
package eval

import (
	"github.com/wippyai/runtime/api/dispatcher"
)

// Command IDs for eval operations.
// Range 230-239 is reserved for eval commands.
const (
	CmdCompile dispatcher.CommandID = 230 // Compile Lua source, return Program handle
	CmdRun     dispatcher.CommandID = 231 // Compile + run, return result
)

func init() {
	dispatcher.MustRegisterCommands("eval",
		CmdCompile, CmdRun,
	)
}

// CompileCmd compiles Lua source code into a reusable Program.
// Yields to dispatcher for rate-limited compilation.
type CompileCmd struct {
	Source  string   // Lua source code
	Method  string   // Method name to execute
	Modules []string // Allowed modules whitelist
}

func (c CompileCmd) CmdID() dispatcher.CommandID {
	return CmdCompile
}

// RunCmd compiles and executes Lua code via the dispatcher.
// Uses the same scheduler as the parent process.
type RunCmd struct {
	Source  string         // Lua source code
	Method  string         // Method name to execute
	Args    []any          // Arguments to pass to method
	Modules []string       // Allowed modules whitelist
	Context map[string]any // Context values to set
}

func (c RunCmd) CmdID() dispatcher.CommandID {
	return CmdRun
}
