package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/queue"
)

func Queue() boot.Component {
	return boot.New(boot.P{
		Name:      LuaQueueName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, queue.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
