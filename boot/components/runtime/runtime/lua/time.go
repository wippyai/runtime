//go:build plugin_lua_time

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/ostime"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
)

func LuaTime() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaTime,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, ostime.NewOSTimeModule(), timemod.NewTimeModule()); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
