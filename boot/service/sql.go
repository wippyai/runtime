//go:build !plugin_minimal

package service

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/boot"
	envapi "github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	bootsystem "github.com/ponyruntime/pony/boot/system"
	"github.com/ponyruntime/pony/service/sql"
)

func SQL() boot.Plugin {
	return boot.New(boot.P{
		Name:      SQLName,
		Phase:     boot.PostInit,
		DependsOn: []string{bootsystem.EnvironmentName},
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
				return ctx, fmt.Errorf("failed to create sql manager: %w", err)
			}

			handlers.RegisterListener("db.sql.*", manager)
			return ctx, nil
		},
	})
}
