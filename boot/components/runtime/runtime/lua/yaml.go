//go:build plugin_lua_yaml

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	bootpkg "github.com/ponyruntime/pony/boot"
	yamlmod "github.com/ponyruntime/pony/runtime/lua/modules/yaml"
)

func LuaYAML() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaYAML,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, yamlmod.NewYAMLModule()); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
