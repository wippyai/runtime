// SPDX-License-Identifier: MPL-2.0

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
			envRegistry := envapi.GetRegistry(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager, err := cdc.NewManager(dtt, bus, logger.Named("cdc"), envRegistry)
			if err != nil {
				return ctx, NewCDCManagerError(err)
			}

			handlers.RegisterListener("db.cdc.*", manager)
			return ctx, nil
		},
	})
}
