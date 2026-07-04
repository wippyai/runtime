// SPDX-License-Identifier: MPL-2.0

package storage

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
	bootpkg "github.com/wippyai/runtime/boot"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	cdc "github.com/wippyai/runtime/service/cdc/postgres"
)

func CDC() boot.Component {
	return boot.New(boot.P{
		Name:      CDCName,
		DependsOn: []boot.Name{bootsystem.EnvironmentName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager, err := cdc.NewManager(dtt, bus, logger.Named("cdc"))
			if err != nil {
				return ctx, NewCDCManagerError(err)
			}

			handlers.RegisterListener("db.cdc.*", manager)
			ctx = cdcapi.WithSourceInspector(ctx, manager)
			ctx = cdcapi.WithSourceStreamer(ctx, manager)
			return ctx, nil
		},
	})
}
