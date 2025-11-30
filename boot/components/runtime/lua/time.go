package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/ostime"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
)

func Time() boot.Component {
	return boot.New(boot.P{
		Name:      LuaTimeName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, ostime.Module, timemod.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
