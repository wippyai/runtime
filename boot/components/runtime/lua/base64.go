//go:build plugin_lua_base64

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/base64"
)

func LuaBase64() boot.Component {
	return boot.New(boot.P{
		Name:      "lua.base64",
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{"lua.engine"},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, base64.NewBase64Module()); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
