package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/function"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/system/function/interceptor"
)

func Interceptor() boot.Component {
	var registry *interceptor.Registry
	return boot.New(boot.P{
		Name:      InterceptorName,
		DependsOn: []boot.ComponentName{},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)

			registry = interceptor.NewInterceptorRegistry(logger.Named("interceptor"))
			ctx = function.WithInterceptorChain(ctx, registry)
			ctx = function.WithInterceptorRegistry(ctx, registry)

			return ctx, nil
		},
	})
}
