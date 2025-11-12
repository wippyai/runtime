//go:build plugin_lua_btea

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea"
)

func LuaBTEA() boot.Component {
	return boot.New(boot.P{
		Name:      "lua.btea",
		Phase:     boot.PostInit,
		DependsOn: []string{"lua.engine"},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				btea.NewBteaModule(logger.Named("btea")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
