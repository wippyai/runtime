//go:build plugin_lua_security

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	securitymod "github.com/ponyruntime/pony/runtime/lua/modules/security"
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
