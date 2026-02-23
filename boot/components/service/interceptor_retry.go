// SPDX-License-Identifier: MPL-2.0

package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	apiinterceptor "github.com/wippyai/runtime/api/function"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/system/function/interceptor/retry"
)

func InterceptorRetry() boot.Component {
	return boot.New(boot.P{
		Name:      InterceptorRetryName,
		DependsOn: []boot.Name{bootsystem.InterceptorName},
		Load: func(ctx context.Context) (context.Context, error) {
			registry := apiinterceptor.GetInterceptorRegistry(ctx)
			if registry == nil {
				return ctx, nil
			}

			if err := registry.Register("retry", retry.New(), 20); err != nil {
				return ctx, err
			}

			return ctx, nil
		},
	})
}
