//go:build plugin_lua_treesitter

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/treesitter"
)

func LuaTreeSitter() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaTreeSitter,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				treesitter.NewTreeSitterModule(logger.Named("tsitter")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
