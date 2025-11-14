//go:build plugin_lua_uuid

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/uuid"
)

func LuaUUID() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaUUID,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm, uuid.NewUUIDModule()); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
