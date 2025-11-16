package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	intapi "github.com/wippyai/runtime/api/interceptor"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/system/interceptor"
)

func Interceptor() boot.Component {
	var registry *interceptor.Registry
	return boot.New(boot.P{
		Name:      InterceptorName,
		Phase:     boot.Init,
		DependsOn: []boot.ComponentName{},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)

			registry = interceptor.NewInterceptorRegistry(logger.Named("interceptor"))
			ctx = intapi.WithChain(ctx, registry)
			ctx = intapi.WithRegistry(ctx, registry)

			return ctx, nil
		},
	})
}
