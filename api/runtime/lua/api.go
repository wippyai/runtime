package lua

import (
	"context"
	"github.com/ponyruntime/go-lua"
)

type (
	Module interface {
		Loader(*lua.LState) int
		Name() string
	}

	Callable interface {
		Execute(ctx context.Context, funcName string, args ...lua.LValue) (lua.LValue, error)
	}
)
