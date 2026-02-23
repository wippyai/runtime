// SPDX-License-Identifier: MPL-2.0

package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/terminal"
)

func Terminal2() boot.Component {
	return boot.New(boot.P{
		Name:      Terminal2Name,
		DependsOn: []boot.Name{system.FactoryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("terminal")
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, ErrEventBusNotAvailable
			}

			dtt := payload.GetTranscoder(ctx)
			if dtt == nil {
				return ctx, ErrTranscoderNotAvailable
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, ErrHandlerRegistryNotAvailable
			}

			factory := process.GetFactory(ctx)
			if factory == nil {
				return ctx, ErrProcessFactoryNotAvailable
			}

			// Get shared dispatcher registry (contains all registered handlers)
			registry := dispatcherapi.GetRegistry(ctx)
			if registry == nil {
				return ctx, ErrDispatcherRegistryNotAvailable
			}

			manager := terminal.NewManager(bus, dtt, registry, factory, logger)
			handlers.RegisterListener("terminal.host", manager)

			logger.Info("terminal manager registered")
			return ctx, nil
		},
	})
}
