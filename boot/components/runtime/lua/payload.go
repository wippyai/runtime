//go:build plugin_lua_payload

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	bootpkg "github.com/wippyai/runtime/boot"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
)

func LuaPayload() boot.Component {
	return boot.New(boot.P{
		Name:      "lua_payload",
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				payloadmod.NewPayloadModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
