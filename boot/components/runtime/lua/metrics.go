package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	metricsmod "github.com/wippyai/runtime/runtime/lua/modules/metrics"
)

func Metrics() boot.Component {
	return boot.New(boot.P{
		Name:      LuaMetricsName,
		DependsOn: []boot.Name{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, metricsmod.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
