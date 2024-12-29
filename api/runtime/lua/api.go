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

	Pool interface {
		Execute(ctx context.Context, name string, args lua.LValue) (lua.LValue, error)
	}
)
