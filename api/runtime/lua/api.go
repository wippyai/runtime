package lua

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"

	lua "github.com/yuin/gopher-lua"
)

type (
	Module interface {
		Loader(*lua.LState) int
		Name() string
	}

	Callable interface {
		Execute(ctx context.Context, funcName string, args ...lua.LValue) (lua.LValue, error)
	}

	Factory interface {
		Compile() error
		MakeVM() (VM, error)
	}

	// Layer represents a processing layer that can handle specific yields
	Layer interface {
		// Step processes tasks and their yields
		// Returns external tasks (ones this layer doesn't handle) and any error
		Step(cvm CVM, tasks ...*engine.Task) ([]*engine.Task, error)
	}

	VM interface {
		Execute(ctx context.Context, name string, args ...lua.LValue) (lua.LValue, error)
		Close()
	}

	// Note that CVM owns the context.
	CVM interface {
		Start(funcName string, args ...lua.LValue) (<-chan engine.TaskResult, error)
		GetTask(thread *lua.LState) (*engine.Task, error)
		Step(tasks ...*engine.Task) ([]*engine.Task, error)
		Close()
	}
)
