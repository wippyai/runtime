//go:build plugin_lua_json

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	jsonmod "github.com/ponyruntime/pony/runtime/lua/modules/json"
)

func LuaJSON() boot.Component {
	return boot.New(boot.P{
		Name:      "lua.json",
		Phase:     boot.PostInit,
		DependsOn: []string{"lua.engine"},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, jsonmod.NewJSONModule()); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
