// SPDX-License-Identifier: MPL-2.0

package store

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	memorystore "github.com/wippyai/runtime/service/store/memory"
)

func MemStore() boot.Component {
	return boot.New(boot.P{
		Name:      MemStoreName,
		DependsOn: nil,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := memorystore.NewManager(
				bus,
				dtt,
				logger.Named("memory"),
			)

			handlers.RegisterListener("store.memory", manager)
			return ctx, nil
		},
	})
}
