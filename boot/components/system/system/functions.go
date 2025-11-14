package system

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	funcapi "github.com/ponyruntime/pony/api/function"
	logapi "github.com/ponyruntime/pony/api/logs"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	regapi "github.com/ponyruntime/pony/api/registry"
	bootcore "github.com/ponyruntime/pony/boot/components/core/core"
	"github.com/ponyruntime/pony/system/function"
	"go.uber.org/zap"
)

func Functions() boot.Component {
	var funcs *function.Registry

	return boot.New(boot.P{
		Name:      FunctionsName,
		Phase:     boot.Init,
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
			funcHost := pubsubapi.GetHost(ctx)
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
		Stop: func(ctx context.Context) error {
			if funcs != nil {
				return funcs.Stop()
			}
			return nil
		},
	})
}
