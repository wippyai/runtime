package lua

import (
	"context"
	httpbase "net/http"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	httpapimod "github.com/wippyai/runtime/runtime/lua/modules/http"
	"github.com/wippyai/runtime/runtime/lua/modules/httpclient"
)

func HTTP() boot.Component {
	return boot.New(boot.P{
		Name:      LuaHTTPName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				httpapimod.NewHTTPAPIModule(logger.Named("http")),
				httpclient.NewHTTPClientModule(logger.Named("http"), httpbase.DefaultClient),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
