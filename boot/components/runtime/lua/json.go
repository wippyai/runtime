package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	jsonmod "github.com/wippyai/runtime/runtime/lua/modules/json"
)

func JSON() boot.Component {
	return boot.New(boot.P{
		Name:      LuaJSONName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			cfg := boot.GetConfig(ctx)
			cacheEnabled := true
			capacity := 1000

			if cfg != nil {
				luaCfg := cfg.Sub("lua")
				if luaCfg != nil {
					jsonCfg := luaCfg.Sub("json")
					if jsonCfg != nil {
						cacheEnabled = jsonCfg.GetBool("cache_enabled", cacheEnabled)
						capacity = jsonCfg.GetInt("capacity", capacity)
					}
				}
			}

			if err := AddModules(ctx, cm, jsonmod.NewJSONModule(
				jsonmod.WithCache(cacheEnabled),
				jsonmod.WithCapacity(capacity),
			)); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
