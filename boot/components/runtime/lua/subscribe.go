//go:build plugin_lua_subscribe

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/runtime/lua/engine/subscribe"
)

func LuaSubscribe() boot.Component {
	return boot.New(boot.P{
		Name:      "lua_subscribe",
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
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
