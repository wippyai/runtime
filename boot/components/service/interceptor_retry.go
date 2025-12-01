package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	apiinterceptor "github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/logs"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/system/function/interceptor/retry"
)

func InterceptorRetry() boot.Component {
	return boot.New(boot.P{
		Name:      InterceptorRetryName,
		DependsOn: []boot.ComponentName{bootsystem.InterceptorName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logs.GetLogger(ctx).Named("interceptor.retry")
			registry := apiinterceptor.GetInterceptorRegistry(ctx)
			if registry == nil {
				return ctx, nil
			}

			if err := registry.Register("retry", retry.NewWithLogger(logger), 20); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
