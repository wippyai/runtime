package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/runtime/lua/modules/funcmod"
	fncallmod "github.com/wippyai/runtime/runtime/lua/modules/funcs"
)

func Func() boot.Component {
	return boot.New(boot.P{
		Name:      LuaFuncName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				fncallmod.NewFunctionModule(),
				funcmod.NewFunctionAPIModule(logger.Named("inbox")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
