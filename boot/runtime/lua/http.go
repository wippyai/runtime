//go:build plugin_lua_http

package lua

import (
	"context"
	httpbase "net/http"

	"github.com/ponyruntime/pony/api/boot"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	httpapimod "github.com/ponyruntime/pony/runtime/lua/modules/http"
	"github.com/ponyruntime/pony/runtime/lua/modules/httpclient"
)

func LuaHTTP() boot.Plugin {
	return boot.New(boot.P{
		Name:      bootpkg.LuaHTTP,
		Phase:     boot.PostInit,
		DependsOn: []string{LuaEngineName},
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
