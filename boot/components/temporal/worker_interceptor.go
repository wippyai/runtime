package temporal

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/service/temporal/interceptor"
)

func WorkerInterceptor() boot.Component {
	return boot.New(boot.P{
		Name:      WorkerInterceptorName,
		DependsOn: []boot.ComponentName{},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available in context")
			}

			registry := interceptor.NewWorkerRegistry()
			ctx = temporalapi.WithWorkerInterceptorRegistry(ctx, registry)

			logger.Info("temporal worker interceptor registry initialized")
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			logger.Info("temporal worker interceptor registry started")
			return nil
		},
		Stop: func(ctx context.Context) error {
			logger := logapi.GetLogger(ctx)
			logger.Info("temporal worker interceptor registry stopped")
			return nil
		},
	})
}
