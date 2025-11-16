//go:build plugin_lua_system

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/system"
)

func LuaSystem() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaSystem,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, system.NewSystemModule()); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
