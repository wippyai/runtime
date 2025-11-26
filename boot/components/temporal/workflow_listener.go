package temporal

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/service/temporal/workflow"
)

func WorkflowListener() boot.Component {
	return boot.New(boot.P{
		Name: WorkflowListenerName,
		DependsOn: []boot.ComponentName{
			WorkerManagerName,
			HostManagerName,
		},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available in context")
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, fmt.Errorf("event bus not available in context")
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, fmt.Errorf("handler registry not available in context")
			}

			workerManager := GetWorkerManager(ctx)
			if workerManager == nil {
				return ctx, fmt.Errorf("worker manager not available in context")
			}

			listener := workflow.NewListener(logger.Named("temporal.workflow"), bus, workerManager)
			handlers.Register(listener)

			logger.Info("temporal workflow listener initialized")
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			logger.Info("temporal workflow listener started")
			return nil
		},
		Stop: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			logger.Info("temporal workflow listener stopped")
			return nil
		},
	})
}
