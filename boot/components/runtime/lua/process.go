package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	processmodapi "github.com/wippyai/runtime/runtime/lua/modules/processmod"
)

func Process() boot.Component {
	return boot.New(boot.P{
		Name:      LuaProcessName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				processmod.NewProcessAPIModule(logger.Named("proc")),
				processmodapi.NewProcessAPIModule(logger.Named("inbox")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
