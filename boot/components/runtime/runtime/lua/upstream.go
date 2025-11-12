//go:build plugin_lua_upstream

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/runtime/lua/engine/upstream"
)

func LuaUpstream() boot.Component {
	return boot.New(boot.P{
		Name:      "lua_upstream",
		Phase:     boot.PostInit,
		DependsOn: []string{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				upstream.NewUpstreamModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
