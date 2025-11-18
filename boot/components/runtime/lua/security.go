package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	securitymod "github.com/wippyai/runtime/runtime/lua/modules/security"
)

func Security() boot.Component {
	return boot.New(boot.P{
		Name:      LuaSecurityName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				securitymod.NewSecurityModule(logger.Named("security")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
