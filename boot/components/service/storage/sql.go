package storage

import (
	"context"
	"github.com/wippyai/runtime/api/boot"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/sql"
)

func SQL() boot.Component {
	return boot.New(boot.P{
		Name:      SQLName,
		DependsOn: []boot.ComponentName{bootsystem.EnvironmentName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			envRegistry := envapi.GetRegistry(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager, err := sql.NewManager(
				dtt,
				bus,
				logger.Named("sql"),
				envRegistry,
			)
			if err != nil {
				return ctx, NewSQLManagerError(err)
			}

			handlers.RegisterListener("db.sql.*", manager)
			return ctx, nil
		},
	})
}
