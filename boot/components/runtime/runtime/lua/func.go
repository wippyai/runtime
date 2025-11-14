//go:build plugin_lua_func

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/funcmod"
	fncallmod "github.com/wippyai/runtime/runtime/lua/modules/funcs"
)

func LuaFunc() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaFunc,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				fncallmod.NewFunctionModule(),
				funcmod.NewFunctionAPIModule(logger.Named("inbox")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
