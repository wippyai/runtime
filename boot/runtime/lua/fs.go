//go:build plugin_lua_fs

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	bootpkg "github.com/ponyruntime/pony/boot"
	fsmod "github.com/ponyruntime/pony/runtime/lua/modules/fs"
)

func LuaFS() boot.Plugin {
	return boot.New(boot.P{
		Name:      bootpkg.LuaFS,
		Phase:     boot.PostInit,
		DependsOn: []string{LuaEngineName},
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
