package executor

import (
	"github.com/ponyruntime/go-lua"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	transcoder "github.com/ponyruntime/pony/pkg/payload/lua"
	"go.uber.org/zap"
)

// Module provides execution capabilities to Lua
type Module struct {
	log *zap.Logger
}

func New(log *zap.Logger) *Module {
	return &Module{log: log}
}

// Name returns the module name
func (m *Module) Name() string {
	return "executor"
}

// Loader is the entry point for loading the plugin
func (m *Module) Loader(l *lua.LState) int {
	t := l.NewTable()

	lapi := map[string]lua.LGFunction{
		"call": m.call,
	}

	l.SetFuncs(t, lapi)
	l.Push(t)
	return 1
}

// call executes a function synchronously
func (m *Module) call(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.ArgError(1, "no context found")
		return 0
	}

	// Get executor from context
	exec, ok := ctx.Value(contextapi.ExecutorCtx).(runtime.Executor)
	if !ok {
		l.ArgError(1, "executor not found in context")
		return 0
	}

	// Check target name
	target := l.CheckString(1)
	if target == "" {
		l.ArgError(1, "target name is required")
		return 0
	}

	// Handle optional argument
	var arg lua.LValue
	if l.GetTop() >= 2 {
		arg = l.CheckAny(2)
	}

	// Create payload if argument is provided
	var p payload.Payload
	if arg != nil {
		p = payload.NewPayload(arg, payload.Lua)
	}

	// Create task
	task := runtime.Task{
		Context: ctx,
		Target:  registry.ID(target),
		Payload: p,
	}

	// Execute task
	resultChan, err := exec.Execute(task)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Handle synchronous execution
	result := <-resultChan
	if result.Error != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(result.Error.Error()))
		return 2
	}

	// Convert result payload to Lua value
	if result.Payload != nil {
		l.Push(transcoder.GoToLua(l, result.Payload.Data()))
	} else {
		l.Push(lua.LNil)
	}
	l.Push(lua.LNil) // No error
	return 2
}
