package temporal

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/service/temporal/activity"
)

func ActivityListener() boot.Component {
	return boot.New(boot.P{
		Name: ActivityListenerName,
		DependsOn: []boot.ComponentName{
			WorkerManagerName,
		},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available in context")
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, fmt.Errorf("handler registry not available in context")
			}

			// Get worker manager from context
			workerManager := GetWorkerManager(ctx)
			if workerManager == nil {
				return ctx, fmt.Errorf("worker manager not available in context")
			}

			// Create activity listener
			listener := activity.NewListener(
				logger.Named("temporal.activity"),
				workerManager,
			)

			// Register as event handler
			handlers.Register(listener)

			logger.Info("temporal activity listener initialized")
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			logger.Info("temporal activity listener started")
			return nil
		},
		Stop: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			logger.Info("temporal activity listener stopped")
			return nil
		},
	})
}
