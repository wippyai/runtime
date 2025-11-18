package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/runtime/lua/modules/websocket"
)

func WebSocket() boot.Component {
	return boot.New(boot.P{
		Name:      LuaWebSocketName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				websocket.NewWebSocketModule(logger.Named("websocket")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
