package system

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	intapi "github.com/ponyruntime/pony/api/interceptor"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/system/interceptor"
)

func Interceptor() boot.Plugin {
	var registry *interceptor.Registry
	return boot.New(boot.P{
		Name:      InterceptorName,
		Phase:     boot.Init,
		DependsOn: []string{"eventbus", "logger"},
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
