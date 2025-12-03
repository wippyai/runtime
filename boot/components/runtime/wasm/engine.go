package wasm

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/dispatchers"
	"github.com/wippyai/runtime/runtime/wasm/component/function"
	reghandler "github.com/wippyai/runtime/system/registry/events"
)

const WasmEngineName boot.ComponentName = "runtime.wasm.engine"

func Engine() boot.Component {
	var manager *function.Manager

	return boot.New(boot.P{
		Name:      WasmEngineName,
		DependsOn: []boot.ComponentName{dispatchers.ClockDispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			disp := dispatcherapi.GetDispatcher(ctx)
			if disp == nil {
				return ctx, ErrDispatcherNotFound
			}

			manager = function.NewManager(
				logger.Named("wasm"),
				bus,
				disp,
			)

			handlers.Register(reghandler.NewRegistryHandler(wasmapi.KindFunction, manager))

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if manager != nil {
				return manager.Start(ctx)
			}
			return nil
		},
		Stop: func(ctx context.Context) error {
			if manager != nil {
				manager.Stop(ctx)
			}
			return nil
		},
	})
}
