package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	ctxmod "github.com/wippyai/runtime/runtime/lua/modules/ctx"
)

func Context() boot.Component {
	return boot.New(boot.P{
		Name:      LuaContextName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			logger := logapi.GetLogger(ctx)
			if err := AddModules(ctx, cm, ctxmod.NewCtxModule(logger.Named("ctx"))); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
