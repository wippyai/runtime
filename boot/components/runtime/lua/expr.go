package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/expr"
)

func Expr() boot.Component {
	return boot.New(boot.P{
		Name:      ExprName,
		DependsOn: []boot.Name{EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			module := expr.NewModule(expr.DefaultOptions())
			if err := AddModules(ctx, cm, module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
