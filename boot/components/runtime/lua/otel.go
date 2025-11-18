package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	otelmod "github.com/wippyai/runtime/runtime/lua/modules/otel"
)

func OTel() boot.Component {
	return boot.New(boot.P{
		Name:      LuaOTelName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				otelmod.NewOTelModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
