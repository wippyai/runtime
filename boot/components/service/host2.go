package service

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process2"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/host2"
	"github.com/wippyai/runtime/system/scheduler/actor"
)

func Host2() boot.Component {
	return boot.New(boot.P{
		Name:      EphemeralHost2Name,
		DependsOn: []boot.ComponentName{system.FactoryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available")
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, fmt.Errorf("event bus not available")
			}

			dtt := payload.GetTranscoder(ctx)
			if dtt == nil {
				return ctx, fmt.Errorf("transcoder not available")
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, fmt.Errorf("handler registry not available")
			}

			factory := process2.GetFactory(ctx)
			if factory == nil {
				return ctx, fmt.Errorf("process factory not available")
			}

			// Create shared actor registry for all hosts
			registry := actor.NewRegistry()

			manager := host2.NewManager(bus, dtt, registry, factory, logger)
			handlers.RegisterListener("process.host", manager)

			logger.Info("host2 manager registered")
			return ctx, nil
		},
	})
}
