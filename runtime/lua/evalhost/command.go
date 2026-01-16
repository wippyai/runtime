package evalhost

import (
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

// Command IDs for eval operations.
const (
	Compile dispatcher.CommandID = 170 // Compile Lua source, return Program handle
	Run     dispatcher.CommandID = 171 // Compile + run, return result
)

func init() {
	dispatcher.MustRegisterCommands("eval",
		Compile, Run,
	)
}

// CompileCmd compiles Lua source code into a reusable Program.
type CompileCmd struct {
	Source       string                 // Lua source code
	Method       string                 // Method name to execute
	Modules      []string               // Allowed modules whitelist
	Imports      map[string]registry.ID // Registry entries to import (alias -> ID)
	AllowClasses []string               // Additional classes to allow (e.g., "process")
}

func (c CompileCmd) CmdID() dispatcher.CommandID {
	return Compile
}

// RunCmd compiles and executes Lua code via the dispatcher.
type RunCmd struct {
	Source        string                 // Lua source code
	Method        string                 // Method name to execute
	Args          payload.Payloads       // Arguments to pass to method
	Modules       []string               // Allowed modules whitelist
	Imports       map[string]registry.ID // Registry entries to import (alias -> ID)
	Context       map[string]any         // Context values to set
	AllowClasses  []string               // Additional classes to allow (e.g., "process")
	CustomModules map[string]any         // Custom Lua tables to inject as modules
	AllowYields   []dispatcher.CommandID // Allowed yield command IDs (empty = no yields)
}

func (c RunCmd) CmdID() dispatcher.CommandID {
	return Run
}
