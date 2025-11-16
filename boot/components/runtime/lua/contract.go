//go:build plugin_lua_contract

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	bootpkg "github.com/wippyai/runtime/boot"
	contractmod "github.com/wippyai/runtime/runtime/lua/modules/contract"
)

func LuaContract() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaContract,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				contractmod.NewContractModule(logger.Named("contract")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
