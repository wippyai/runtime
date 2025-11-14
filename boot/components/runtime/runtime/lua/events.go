//go:build plugin_lua_events

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/events"
)

func LuaEvents() boot.Component {
	return boot.New(boot.P{
		Name:      "lua_events",
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				events.NewEventsModule(logger.Named("events")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
