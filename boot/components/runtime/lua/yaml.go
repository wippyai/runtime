package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	yamlmod "github.com/wippyai/runtime/runtime/lua/modules/yaml"
)

func YAML() boot.Component {
	return boot.New(boot.P{
		Name:      LuaYAMLName,
		DependsOn: []boot.Name{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, yamlmod.Module); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
