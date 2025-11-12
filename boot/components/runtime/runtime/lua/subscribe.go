//go:build plugin_lua_subscribe

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
)

func LuaSubscribe() boot.Component {
	return boot.New(boot.P{
		Name:      "lua_subscribe",
		Phase:     boot.PostInit,
		DependsOn: []string{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				subscribe.NewSubscribeModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
