//go:build plugin_lua_expr

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/expr"
)

func LuaExpr() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaExpr,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				expr.NewExprModule(expr.WithCapacity(5000)),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
