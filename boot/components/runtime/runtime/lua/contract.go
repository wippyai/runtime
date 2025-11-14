//go:build plugin_lua_contract

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	contractmod "github.com/ponyruntime/pony/runtime/lua/modules/contract"
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
