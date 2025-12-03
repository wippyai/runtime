package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	registrymod "github.com/wippyai/runtime/runtime/lua/modules/registry"
)

func Registry() boot.Component {
	return boot.New(boot.P{
		Name:      LuaRegistryName,
		DependsOn: []boot.Name{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, registrymod.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
