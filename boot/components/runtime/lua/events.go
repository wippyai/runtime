package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/events"
)

func Events() boot.Component {
	return boot.New(boot.P{
		Name:      LuaEventsName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, events.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
