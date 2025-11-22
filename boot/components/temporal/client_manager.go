package temporal

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/service/temporal/client"
	"go.uber.org/zap"
)

func ClientManager() boot.Component {
	return boot.New(boot.P{
		Name: ClientManagerName,
		DependsOn: []boot.ComponentName{
			ClientInterceptorName,
			DataConverterName,
		},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available in context")
			}

			transcoder := payload.GetTranscoder(ctx)
			if transcoder == nil {
				return ctx, fmt.Errorf("transcoder not available in context")
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, fmt.Errorf("event bus not available in context")
			}

			envRegistry := envapi.GetRegistry(ctx)
			if envRegistry == nil {
				return ctx, fmt.Errorf("env registry not available in context")
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, fmt.Errorf("handler registry not available in context")
			}

			// Get registries from context
			clientInterceptorRegistry := temporalapi.GetClientInterceptorRegistry(ctx)
			if clientInterceptorRegistry == nil {
				return ctx, fmt.Errorf("client interceptor registry not available in context")
			}

			dataConverterRegistry := temporalapi.GetDataConverterRegistry(ctx)
			if dataConverterRegistry == nil {
				return ctx, fmt.Errorf("data converter registry not available in context")
			}

			// Build final data converter
			dataConverter := dataConverterRegistry.Build()

			// Get client interceptors
			clientInterceptors := clientInterceptorRegistry.GetAll()

			// Create client manager
			manager, err := client.NewManager(
				logger.Named("temporal"),
				transcoder,
				bus,
				envRegistry,
				dataConverter,
				clientInterceptors,
			)
			if err != nil {
				return ctx, fmt.Errorf("failed to create temporal client manager: %w", err)
			}

			// Register manager as listener for temporal.client entries
			handlers.RegisterListener("temporal.client", manager)

			// Register temporal client dependency patterns
			reg := regapi.GetRegistry(ctx)
			if reg != nil {
				if err := reg.RegisterDependencyPattern(regapi.DependencyPattern{
					Path:        "data.client",
					Description: "Reference to temporal client",
				}); err != nil {
					logger.Warn("failed to register temporal client dependency pattern", zap.Error(err))
				}
			}

			logger.Info("temporal client manager initialized")
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			logger.Info("temporal client manager started")
			return nil
		},
		Stop: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			logger.Info("temporal client manager stopped")
			return nil
		},
	})
}
