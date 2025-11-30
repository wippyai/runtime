package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	streammod "github.com/wippyai/runtime/runtime/lua/modules/stream"
)

func Stream() boot.Component {
	return boot.New(boot.P{
		Name:      LuaStreamName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, streammod.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
