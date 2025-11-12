//go:build plugin_lua_process

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	processmod "github.com/ponyruntime/pony/runtime/lua/modules/process"
	processmodapi "github.com/ponyruntime/pony/runtime/lua/modules/processmod"
)

func LuaProcess() boot.Plugin {
	return boot.New(boot.P{
		Name:      bootpkg.LuaProcess,
		Phase:     boot.PostInit,
		DependsOn: []string{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				processmod.NewProcessAPIModule(logger.Named("proc")),
				processmodapi.NewProcessAPIModule(logger.Named("inbox")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
