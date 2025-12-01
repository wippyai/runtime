package wasm

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	logapi "github.com/wippyai/runtime/api/logs"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/dispatchers"
	"github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/runtime/wasm/component/component"
	reghandler "github.com/wippyai/runtime/system/registry/events"
)

const WasmComponentName boot.ComponentName = "runtime.wasm.component"

func Component() boot.Component {
	var manager *component.Manager

	return boot.New(boot.P{
		Name:      WasmComponentName,
		DependsOn: []boot.ComponentName{dispatchers.ClockDispatcherName, system.FilesystemName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			disp := dispatcherapi.GetDispatcher(ctx)
			if disp == nil {
				return ctx, fmt.Errorf("dispatcher not found in context")
			}

			fsReg := fsapi.GetRegistry(ctx)
			if fsReg == nil {
				return ctx, fmt.Errorf("filesystem registry not found in context")
			}

			manager = component.NewManager(
				logger.Named("wasm.component"),
				bus,
				disp,
				fsReg,
			)

			handlers.Register(reghandler.NewRegistryHandler(wasmapi.KindComponentFunction, manager))

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
