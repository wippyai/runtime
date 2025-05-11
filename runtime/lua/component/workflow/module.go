package workflow

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/command"
	lua "github.com/yuin/gopher-lua"
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
	// Create module table
	mod := l.CreateTable(0, 5)

	command.RegisterCommand(l)

	// Register functions
	mod.RawSetString("request", l.NewFunction(addCommandFunc))

	// Push the module
	l.Push(mod)
	return 1
}

// addCommandFunc adds a command to the workflow's pipeline
// Params: command
func addCommandFunc(l *lua.LState) int {
	//// Get command from arguments
	//cmdValue := l.CheckAny(1)
	//cmd := commandToRuntime(l, cmdValue)
	//if cmd == nil {
	//	l.ArgError(1, "command expected")
	//	return 0
	//}
	//
	//// Get workflow from context
	//workflow := getWorkflow(l) // todo use proper context
	//if workflow == nil {
	//	return 0
	//}
	//
	//// Add the command to the workflow
	//workflow.AddCommand(cmd)
	//
	//// Return success
	//l.Push(lua.LBool(true))
	return 1
}
