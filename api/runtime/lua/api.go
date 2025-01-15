package lua

import (
	"context"

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
)
