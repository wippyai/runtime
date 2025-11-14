//go:build plugin_lua_security

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	bootpkg "github.com/wippyai/runtime/boot"
	securitymod "github.com/wippyai/runtime/runtime/lua/modules/security"
)

func LuaSecurity() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaSecurity,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				securitymod.NewSecurityModule(logger.Named("security")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
