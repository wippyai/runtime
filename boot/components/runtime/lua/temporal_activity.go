package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/temporal/activity"
)

func TemporalActivity() boot.Component {
	return boot.New(boot.P{
		Name:      LuaTemporalActivityName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, activity.NewModule()); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
