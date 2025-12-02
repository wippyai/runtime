package evalhost

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
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
	Source  string         // Lua source code
	Method  string         // Method name to execute
	Args    []any          // Arguments to pass to method
	Modules []string       // Allowed modules whitelist
	Context map[string]any // Context values to set
}

func (c RunCmd) CmdID() dispatcher.CommandID {
	return CmdRun
}

// Context helpers

var evalHostKey = &ctxapi.Key{Name: "eval.host"}

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
