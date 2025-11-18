package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	contractmod "github.com/wippyai/runtime/runtime/lua/modules/contract"
)

func Contract() boot.Component {
	return boot.New(boot.P{
		Name:      LuaContractName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				contractmod.NewContractModule(logger.Named("contract")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
