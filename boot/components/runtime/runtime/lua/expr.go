//go:build plugin_lua_expr

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/runtime/lua/modules/expr"
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
