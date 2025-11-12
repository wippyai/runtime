//go:build plugin_lua_websocket

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/runtime/lua/modules/websocket"
)

func LuaWebSocket() boot.Plugin {
	return boot.New(boot.P{
		Name:      bootpkg.LuaWebSocket,
		Phase:     boot.PostInit,
		DependsOn: []string{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				websocket.NewWebSocketModule(logger.Named("websocket")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
