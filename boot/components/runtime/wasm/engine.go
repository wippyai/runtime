package wasm

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	logapi "github.com/wippyai/runtime/api/logs"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/dispatchers"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmfunc "github.com/wippyai/runtime/runtime/wasm/component/function"
)

// Engine wires WASM function registry handling and runtime lifecycle.
func Engine() boot.Component {
	var funcs *wasmfunc.Manager

	return boot.New(boot.P{
		Name:      EngineName,
		DependsOn: []boot.Name{dispatchers.ClockDispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			disp := dispatcherapi.GetDispatcher(ctx)
			if disp == nil {
				return ctx, ErrDispatcherNotFound
			}

			fsReg := fsapi.GetRegistry(ctx)
			funcs = wasmfunc.NewManager(
				logger.Named("wasm.func"),
				bus,
				disp,
				fsReg,
			)

			handlers.Register(wasmcomponent.NewHandler("function.(wasm|wat)", funcs))

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if funcs != nil {
				return funcs.Start(ctx)
			}
			return nil
		},
		Stop: func(_ context.Context) error {
			if funcs != nil {
				funcs.Stop()
			}
			return nil
		},
	})
}
