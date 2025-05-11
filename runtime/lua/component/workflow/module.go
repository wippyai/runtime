package workflow

import (
	"context"
	lua "github.com/yuin/gopher-lua"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/runtime/lua/command"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
)

// Module represents the workflow Lua module
type Module struct{}

// NewModule creates a new workflow module
func NewModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "workflow"
}

// Execution context key
type contextKey struct{ name string }

var (
	execContextKey = &contextKey{"workflow.execContext"}
)

// WithExecContext adds execution context to the context
func WithExecContext(ctx context.Context, execCtx interface{}) context.Context {
	return context.WithValue(ctx, execContextKey, execCtx)
}

// GetExecContext retrieves execution context from the context
func GetExecContext(ctx context.Context) interface{} {
	return ctx.Value(execContextKey)
}

// Loader registers the module functions
func (m *Module) Loader(l *lua.LState) int {
	command.RegisterCommand(l)

	mod := l.CreateTable(0, 1)
	mod.RawSetString("request", l.NewFunction(addCommandFunc))
	l.Push(mod)

	return 1
}

// addCommandFunc adds a command to the workflow's pipeline
// Params: command
func addCommandFunc(l *lua.LState) int {
	// Get command from arguments
	cmdValue := l.CheckAny(1)
	cmd := commandToRuntime(l, cmdValue)
	if cmd == nil {
		l.ArgError(1, "command expected")
		return 0
	}

	// Get unit of work from context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work context found")
		return 0
	}

	// Get command queue from unit of work
	queue := GetCommandQueue(uw)
	if queue == nil {
		l.RaiseError("command queue not available")
		return 0
	}

	// Add the command to the queue
	queue.Push(cmd)

	// Wake up the unit of work to process the new command
	uw.Tasks().WakeUp()

	// Return success
	l.Push(lua.LBool(true))
	return 1
}

// commandToRuntime converts a Lua value to a runtime.Command
func commandToRuntime(l *lua.LState, value lua.LValue) runtime.Command {
	// For table representation of a command
	if table, ok := value.(*lua.LTable); ok {
		cmdType := lua.LVAsString(table.RawGetString("type"))
		if cmdType == "" {
			l.RaiseError("command must have a type")
			return nil
		}

		// Extract parameters if any
		var params []payload.Payload
		paramsTable := table.RawGetString("params")
		if paramsTable != lua.LNil {
			if pt, ok := paramsTable.(*lua.LTable); ok {
				pt.ForEach(func(_, v lua.LValue) {
					// Convert Lua value to payload
					params = append(params, luaconv.ExportPayload(v))
				})
			}
		}

		// Create a new command
		return command.NewCommand(l, cmdType, nil, params...)
	}

	// For userdata representation (already a command)
	if ud, ok := value.(*lua.LUserData); ok {
		if cmd, ok := ud.Value.(runtime.Command); ok {
			return cmd
		}
	}

	return nil
}
