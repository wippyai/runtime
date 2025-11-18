package system

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	funcapi "github.com/wippyai/runtime/api/function"
	logapi "github.com/wippyai/runtime/api/logs"
	regapi "github.com/wippyai/runtime/api/registry"
	relayapi "github.com/wippyai/runtime/api/relay"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	"github.com/wippyai/runtime/system/function"
	"go.uber.org/zap"
)

func Functions() boot.Component {
	var funcs *function.Registry

	return boot.New(boot.P{
		Name:      FunctionsName,
		DependsOn: []boot.ComponentName{bootcore.RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available in context")
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, fmt.Errorf("event bus not available in context")
			}

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, fmt.Errorf("registry not available in context")
			}

			// Register function dependency pattern
			if err := reg.RegisterDependencyPattern(regapi.DependencyPattern{
				Path:        "data.func",
				Description: "Reference to handler function",
			}); err != nil {
				logger.Warn("failed to register function dependency pattern", zap.Error(err))
			}

			// Function host is already registered in infrastructure
			funcHost := relayapi.GetHost(ctx)
			if funcHost != nil {
				funcs = function.NewFunctionRegistry(bus, funcHost, logger.Named("funcs"))
			}

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
