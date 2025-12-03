package queue

import (
	"context"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	queueapi "github.com/wippyai/runtime/api/queue"
	regapi "github.com/wippyai/runtime/api/registry"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	queuemgr "github.com/wippyai/runtime/system/queue"
	"github.com/wippyai/runtime/system/queue/interceptor"
	"go.uber.org/zap"
)

func Manager() boot.Component {
	var mgr *queuemgr.Manager
	var interceptorReg *interceptor.Registry

	return boot.New(boot.P{
		Name:      ManagerName,
		DependsOn: []boot.Name{bootcore.RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, ErrEventBusNotAvailable
			}

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, ErrRegistryNotAvailable
			}

			if err := reg.RegisterDependencyPattern(regapi.DependencyPattern{
				Path:        "data.queue",
				Description: "Reference to queue",
			}); err != nil {
				logger.Warn("failed to register queue dependency pattern", zap.Error(err))
			}

			interceptorReg = interceptor.NewInterceptorRegistry(logger.Named("queue.interceptor"))

			mgr = queuemgr.NewManager(bus, logger.Named("queue"))

			interceptorReg.SetPublishFunc(mgr.PublishDirect)
			mgr.SetPublishChain(interceptorReg)

			ctx = queueapi.WithManager(ctx, mgr)
			ctx = queueapi.WithPublishChain(ctx, interceptorReg)
			ctx = queueapi.WithPublishInterceptorRegistry(ctx, interceptorReg)

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if mgr != nil {
				return mgr.Start(ctx)
			}
			return nil
		},
		Stop: func(_ context.Context) error {
			if mgr != nil {
				return mgr.Stop()
			}
			return nil
		},
	})
}
