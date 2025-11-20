package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	evalmod "github.com/wippyai/runtime/runtime/lua/modules/eval"
)

func Eval() boot.Component {
	return boot.New(boot.P{
		Name:      LuaEvalName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				evalmod.NewEvalModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
