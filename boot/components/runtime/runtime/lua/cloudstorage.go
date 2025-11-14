//go:build plugin_lua_cloudstorage

package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/runtime/lua/modules/cloudstorage"
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
