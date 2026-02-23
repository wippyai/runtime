// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	funcapi "github.com/wippyai/runtime/api/function"
	logapi "github.com/wippyai/runtime/api/logs"
	regapi "github.com/wippyai/runtime/api/registry"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	"github.com/wippyai/runtime/system/function"
	"go.uber.org/zap"
)

func Functions() boot.Component {
	var funcs *function.Registry

	return boot.New(boot.P{
		Name:      FunctionsName,
		DependsOn: []boot.Name{bootcore.RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("funcs")
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, ErrEventBusNotAvailable
			}

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, ErrRegistryNotAvailable
			}

			// Register function dependency pattern
			if err := reg.RegisterDependencyPattern(regapi.DependencyPattern{
				Path:        "data.func",
				Description: "Reference to handler function",
			}); err != nil {
				logger.Warn("failed to register function dependency pattern", zap.Error(err))
			}

			// Create function registry (host is retrieved from frame context at call time)
			funcs = function.NewFunctionRegistry(bus, logger.Named("funcs"))

			return funcapi.WithRegistry(ctx, funcs), nil
		},
		Start: func(ctx context.Context) error {
			if funcs != nil {
				return funcs.Start(ctx)
			}
			return nil
		},
		Stop: func(_ context.Context) error {
			if funcs != nil {
				return funcs.Stop()
			}
			return nil
		},
	})
}
