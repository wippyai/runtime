// SPDX-License-Identifier: MPL-2.0

package storage

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	resourceapi "github.com/wippyai/runtime/api/resource"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
	bootpkg "github.com/wippyai/runtime/boot"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	cdcservice "github.com/wippyai/runtime/service/cdc"
	pgcdc "github.com/wippyai/runtime/service/cdc/postgres"
	sqlitecdc "github.com/wippyai/runtime/service/cdc/sqlite"
)

func CDC() boot.Component {
	return boot.New(boot.P{
		Name:      CDCName,
		DependsOn: []boot.Name{bootsystem.EnvironmentName, bootsystem.ResourcesName, bootsystem.CDCRegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			resReg := resourceapi.GetRegistry(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)
			if cdcapi.GetRegistry(ctx) == nil {
				return ctx, NewCDCManagerError(errors.New("cdc system registry not available"))
			}

			cdcRegistry, ok := cdcapi.GetRegistry(ctx).(cdcservice.Registry)
			if !ok {
				return ctx, NewCDCManagerError(errors.New("cdc system registry has an unsupported implementation"))
			}
			manager, err := cdcservice.NewManager(
				cdcRegistry,
				dtt,
				bus,
				resReg,
				logger.Named("cdc"),
				cdcservice.WithDriver(pgcdc.NewDriver(), sqlitecdc.NewDriver()),
			)
			if err != nil {
				return ctx, NewCDCManagerError(err)
			}

			handlers.RegisterListener("db.cdc.postgres", manager)
			handlers.RegisterListener("db.cdc.sqlite", manager)
			return ctx, nil
		},
	})
}
