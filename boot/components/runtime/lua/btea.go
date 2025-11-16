//go:build plugin_lua_btea

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/runtime/lua/modules/btea"
)

func LuaBTEA() boot.Component {
	return boot.New(boot.P{
		Name:      "lua.btea",
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{"lua.engine"},
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
