package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/service/interceptor"
)

func InterceptorManager() boot.Component {
	return boot.New(boot.P{
		Name:      InterceptorManagerName,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{},
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
