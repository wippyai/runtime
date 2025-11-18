package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	sqlmod "github.com/wippyai/runtime/runtime/lua/modules/sql"
)

func SQL() boot.Component {
	return boot.New(boot.P{
		Name:      LuaSQLName,
		DependsOn: []boot.ComponentName{LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, nil
			}

			if err := AddModules(ctx, cm,
				sqlmod.NewSQLModule(logger.Named("sql")),
			); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
