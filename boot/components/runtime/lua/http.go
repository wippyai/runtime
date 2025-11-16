//go:build plugin_lua_http

package lua

import (
	"context"
	httpbase "net/http"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	bootpkg "github.com/wippyai/runtime/boot"
	httpapimod "github.com/wippyai/runtime/runtime/lua/modules/http"
	"github.com/wippyai/runtime/runtime/lua/modules/httpclient"
)

func LuaHTTP() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaHTTP,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				httpapimod.NewHTTPAPIModule(logger.Named("http")),
				httpclient.NewHTTPClientModule(logger.Named("http"), httpbase.DefaultClient),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
