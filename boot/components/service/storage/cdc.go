// SPDX-License-Identifier: MPL-2.0

package storage

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	resourceapi "github.com/wippyai/runtime/api/resource"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
	bootpkg "github.com/wippyai/runtime/boot"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	pgcdc "github.com/wippyai/runtime/service/cdc/postgres"
	sqlitecdc "github.com/wippyai/runtime/service/cdc/sqlite"
)

func CDC() boot.Component {
	return boot.New(boot.P{
		Name:      CDCName,
		DependsOn: []boot.Name{bootsystem.EnvironmentName, bootsystem.ResourcesName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			resReg := resourceapi.GetRegistry(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			pgManager, err := pgcdc.NewManager(dtt, bus, logger.Named("cdc.postgres"))
			if err != nil {
				return ctx, NewCDCManagerError(err)
			}

			sqliteManager, err := sqlitecdc.NewManager(dtt, bus, logger.Named("cdc.sqlite"), resReg)
			if err != nil {
				return ctx, NewCDCManagerError(err)
			}

			handlers.RegisterListener("db.cdc.postgres", pgManager)
			handlers.RegisterListener("db.cdc.sqlite", sqliteManager)

			composite := cdcapi.NewComposite(pgManager, sqliteManager)
			ctx = cdcapi.WithSourceInspector(ctx, composite)
			ctx = cdcapi.WithSourceStreamer(ctx, composite)
			return ctx, nil
		},
	})
}
