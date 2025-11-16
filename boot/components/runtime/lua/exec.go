//go:build plugin_lua_exec

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/exec"
)

func LuaExec() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaExec,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				exec.NewExecModule(logger.Named("exec")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
