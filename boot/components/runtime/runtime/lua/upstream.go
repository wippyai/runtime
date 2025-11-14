//go:build plugin_lua_upstream

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/runtime/lua/engine/upstream"
)

func LuaUpstream() boot.Component {
	return boot.New(boot.P{
		Name:      "lua_upstream",
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
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
