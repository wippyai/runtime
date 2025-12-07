package evalhost

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
)

// Command IDs for eval operations.
// Range 230-239 is reserved for eval commands.
const (
	CmdCompile       dispatcher.CommandID = 230 // Compile Lua source, return Program handle
	CmdRun           dispatcher.CommandID = 231 // Compile + run, return result
	CmdCreateProcess dispatcher.CommandID = 232 // Create steppable process from Program
)

func init() {
	dispatcher.MustRegisterCommands("eval",
		CmdCompile, CmdRun, CmdCreateProcess,
	)
}

// CompileCmd compiles Lua source code into a reusable Program.
type CompileCmd struct {
	Source  string   // Lua source code
	Method  string   // Method name to execute
	Modules []string // Allowed modules whitelist
}

func (c CompileCmd) CmdID() dispatcher.CommandID {
	return CmdCompile
}

// RunCmd compiles and executes Lua code via the dispatcher.
type RunCmd struct {
	Source  string           // Lua source code
	Method  string           // Method name to execute
	Args    payload.Payloads // Arguments to pass to method
	Modules []string         // Allowed modules whitelist
	Context map[string]any   // Context values to set
}

func (c RunCmd) CmdID() dispatcher.CommandID {
	return CmdRun
}

// CreateProcessCmd creates a steppable process from a compiled Program.
type CreateProcessCmd struct {
	Program *Program // Compiled program
}

func (c CreateProcessCmd) CmdID() dispatcher.CommandID {
	return CmdCreateProcess
}

// Context helpers

var evalHostKey = &ctxapi.Key{Name: "eval.host"} // todO: why we need it on context if we have dispatcher?

// WithHost attaches an eval Host to the application context.
func WithHost(ctx context.Context, host *Host) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(evalHostKey) == nil {
		ac.With(evalHostKey, host)
	}
	return ctx
}

// GetHost retrieves the eval Host from the context.
func GetHost(ctx context.Context) *Host {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if h := ac.Get(evalHostKey); h != nil {
		return h.(*Host)
	}
	return nil
}
