package temporal

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/service/temporal/worker"
	"go.uber.org/zap"
)

func WorkerManager() boot.Component {
	return boot.New(boot.P{
		Name: WorkerManagerName,
		DependsOn: []boot.ComponentName{
			WorkerInterceptorName,
			ClientManagerName,
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

			registry := resource.GetRegistry(ctx)
			if registry == nil {
				return ctx, fmt.Errorf("resource registry not available in context")
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, fmt.Errorf("handler registry not available in context")
			}

			// Get worker interceptor registry
			workerInterceptorRegistry := temporalapi.GetWorkerInterceptorRegistry(ctx)
			if workerInterceptorRegistry == nil {
				return ctx, fmt.Errorf("worker interceptor registry not available in context")
			}

			// Get worker interceptors
			workerInterceptors := workerInterceptorRegistry.GetAll()

			// Create worker manager
			manager, err := worker.NewManager(
				logger.Named("temporal.worker"),
				transcoder,
				bus,
				registry,
				workerInterceptors,
			)
			if err != nil {
				return ctx, fmt.Errorf("failed to create temporal worker manager: %w", err)
			}

			// Store manager in context for activity listener
			ctx = context.WithValue(ctx, workerManagerKey{}, manager)

			// Register manager as listener for temporal.worker entries
			handlers.RegisterListener("temporal.worker", manager)

			// Register temporal worker dependency patterns
			reg := regapi.GetRegistry(ctx)
			if reg != nil {
				workerPatterns := []regapi.DependencyPattern{
					{Path: "data.client", Description: "Reference to temporal client in worker config"},
					{Path: "meta.temporal.activity.worker", Description: "Worker reference in activity metadata"},
					{Path: "meta.temporal.workflow.worker", Description: "Worker reference in workflow metadata"},
				}
				for _, pattern := range workerPatterns {
					if err := reg.RegisterDependencyPattern(pattern); err != nil {
						logger.Warn("failed to register temporal worker dependency pattern",
							zap.String("path", pattern.Path),
							zap.Error(err))
					}
				}
			}

			logger.Info("temporal worker manager initialized")
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			logger.Info("temporal worker manager started")
			return nil
		},
		Stop: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			logger.Info("temporal worker manager stopped")
			return nil
		},
	})
}

type workerManagerKey struct{}

// GetWorkerManager retrieves the worker manager from context
func GetWorkerManager(ctx context.Context) *worker.Manager {
	if v := ctx.Value(workerManagerKey{}); v != nil {
		if manager, ok := v.(*worker.Manager); ok {
			return manager
		}
	}
	return nil
}
