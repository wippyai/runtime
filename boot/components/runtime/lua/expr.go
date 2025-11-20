package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/expr"
)

func Expr() boot.Component {
	return boot.New(boot.P{
		Name:      LuaExprName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			cfg := boot.GetConfig(ctx)
			cacheEnabled := true
			capacity := 5000

			if cfg != nil {
				luaCfg := cfg.Sub("lua")
				if luaCfg != nil {
					exprCfg := luaCfg.Sub("expr")
					if exprCfg != nil {
						cacheEnabled = exprCfg.GetBool("cache_enabled", cacheEnabled)
						capacity = exprCfg.GetInt("capacity", capacity)
					}
				}
			}

			if err := AddModules(ctx, cm,
				expr.NewExprModule(
					expr.WithCache(cacheEnabled),
					expr.WithCapacity(capacity),
				),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
