package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	registrymod "github.com/wippyai/runtime/runtime/lua/modules/registry"
)

func Registry() boot.Component {
	return boot.New(boot.P{
		Name:      LuaRegistryName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				registrymod.NewLoaderModule(logger.Named("loader")),
				registrymod.NewRegistryModule(logger.Named("registry")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
