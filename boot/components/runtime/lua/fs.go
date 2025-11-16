//go:build plugin_lua_fs

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	bootpkg "github.com/wippyai/runtime/boot"
	fsmod "github.com/wippyai/runtime/runtime/lua/modules/fs"
)

func LuaFS() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaFS,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				fsmod.NewFSModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
