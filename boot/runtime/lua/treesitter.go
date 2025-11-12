//go:build plugin_lua_treesitter

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/runtime/lua/modules/treesitter"
)

func LuaTreeSitter() boot.Plugin {
	return boot.New(boot.P{
		Name:      bootpkg.LuaTreeSitter,
		Phase:     boot.PostInit,
		DependsOn: []string{LuaEngineName},
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
