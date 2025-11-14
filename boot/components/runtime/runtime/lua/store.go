//go:build plugin_lua_store

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/runtime/lua/modules/store"
)

func LuaStore() boot.Component {
	return boot.New(boot.P{
		Name:      bootpkg.LuaStore,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				store.NewStoreModule(logger.Named("store")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
