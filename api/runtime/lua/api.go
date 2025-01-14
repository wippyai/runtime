package lua

import (
	"context"
	"github.com/yuin/gopher-lua"
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

	VM interface {
		Execute(ctx context.Context, name string, args ...lua.LValue) (lua.LValue, error)
		Close()
	}
)
