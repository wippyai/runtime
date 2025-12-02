package service

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process2"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/terminal2"
)

func Terminal2() boot.Component {
	return boot.New(boot.P{
		Name:      Terminal2Name,
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

			// Get shared dispatcher registry (contains all registered handlers)
			registry := dispatcherapi.GetRegistry(ctx)
			if registry == nil {
				return ctx, fmt.Errorf("dispatcher registry not available")
			}

			manager := terminal2.NewManager(bus, dtt, registry, factory, logger)
			handlers.RegisterListener("terminal.host2", manager)

			logger.Info("terminal2 manager registered")
			return ctx, nil
		},
	})
}
