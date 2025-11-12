//go:build plugin_lua_payload

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	bootpkg "github.com/ponyruntime/pony/boot"
	payloadmod "github.com/ponyruntime/pony/runtime/lua/modules/payload"
)

func LuaPayload() boot.Component {
	return boot.New(boot.P{
		Name:      "lua_payload",
		Phase:     boot.PostInit,
		DependsOn: []string{LuaEngineName},
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
