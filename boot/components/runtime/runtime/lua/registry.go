//go:build plugin_lua_registry

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	registrymod "github.com/ponyruntime/pony/runtime/lua/modules/registry"
)

func LuaRegistry() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaRegistry,
		Phase:     boot.PostInit,
		DependsOn: []string{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				registrymod.NewLoaderModule(logger.Named("loader")),
				registrymod.NewRegistryModule(logger.Named("registry")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
