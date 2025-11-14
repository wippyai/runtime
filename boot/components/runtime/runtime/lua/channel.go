//go:build plugin_lua_channel

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
)

func LuaChannel() boot.Component {
	return boot.New(boot.P{
		Name:      "lua_channel",
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{"lua.engine"},
		Load: func(ctx context.Context) (context.Context, error) {
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				channel.NewChannelModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
