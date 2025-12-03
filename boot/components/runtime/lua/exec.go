package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/exec"
)

func Exec() boot.Component {
	return boot.New(boot.P{
		Name:      LuaExecName,
		DependsOn: []boot.Name{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, exec.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
