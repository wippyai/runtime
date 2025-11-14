//go:build plugin_lua_cloudstorage

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/runtime/lua/modules/cloudstorage"
)

func LuaCloudStorage() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaCloudStorage,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				cloudstorage.NewModule(),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
