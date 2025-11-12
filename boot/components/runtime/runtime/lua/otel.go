//go:build plugin_lua_otel

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	bootpkg "github.com/ponyruntime/pony/boot"
	otelmod "github.com/ponyruntime/pony/runtime/lua/modules/otel"
)

func LuaOTel() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaOTel,
		Phase:     boot.PostInit,
		DependsOn: []string{LuaEngineName},
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
