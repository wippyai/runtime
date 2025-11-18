package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
)

func Channel() boot.Component {
	return boot.New(boot.P{
		Name:      LuaChannelName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				channel.NewChannelModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
