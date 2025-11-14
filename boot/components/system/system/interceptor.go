package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
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
			bus := event.GetBus(ctx)

			registry = interceptor.NewInterceptorRegistry(bus, logger.Named("interceptor"))
			return intapi.WithChain(ctx, registry), nil
		},
		Start: func(ctx context.Context) error {
			if registry != nil {
				return registry.Start(ctx)
			}
			return nil
		},
		Stop: func(ctx context.Context) error {
			if registry != nil {
				return registry.Stop()
			}
			return nil
		},
	})
}
