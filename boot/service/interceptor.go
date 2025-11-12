//go:build !plugin_minimal

package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/service/interceptor"
)

func InterceptorManager() boot.Plugin {
	return boot.New(boot.P{
		Name:      InterceptorManagerName,
		Phase:     boot.PostInit,
		DependsOn: []string{"eventbus", "logger"},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			factory := interceptor.NewDefaultFactory()
			manager := factory.CreateManager(bus, logger.Named("interceptor"))
			handlers.RegisterListener("interceptor.*", manager)
			return ctx, nil
		},
	})
}
