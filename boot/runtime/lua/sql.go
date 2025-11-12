//go:build plugin_lua_sql

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	sqlmod "github.com/ponyruntime/pony/runtime/lua/modules/sql"
)

func LuaSQL() boot.Plugin {
	return boot.New(boot.P{
		Name:      bootpkg.LuaSQL,
		Phase:     boot.PostInit,
		DependsOn: []string{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			codeManager := GetCodeManager(ctx)

			if err := AddModules(ctx, codeManager,
				sqlmod.NewSQLModule(logger.Named("sql")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
