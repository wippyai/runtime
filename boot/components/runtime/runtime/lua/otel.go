//go:build plugin_lua_otel

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	bootpkg "github.com/wippyai/runtime/boot"
	otelmod "github.com/wippyai/runtime/runtime/lua/modules/otel"
)

func LuaOTel() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaOTel,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				otelmod.NewOTelModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
