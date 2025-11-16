//go:build plugin_lua_process

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	bootpkg "github.com/wippyai/runtime/boot"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	processmodapi "github.com/wippyai/runtime/runtime/lua/modules/processmod"
)

func LuaProcess() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaProcess,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
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
