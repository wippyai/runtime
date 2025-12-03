package wasm

import (
	"context"

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
	"github.com/wippyai/runtime/runtime/wasm/transport"
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
			cfg := boot.GetConfig(ctx)

			disp := dispatcherapi.GetDispatcher(ctx)
			if disp == nil {
				return ctx, ErrDispatcherNotFound
			}

			fsReg := fsapi.GetRegistry(ctx)
			if fsReg == nil {
				return ctx, ErrFilesystemNotFound
			}

			// Get WASM config with defaults
			wasmCfg := component.DefaultConfig()
			if cfg != nil {
				wasmSub := cfg.Sub("wasm")
				if wasmSub != nil {
					wasmCfg.StrictMode = wasmSub.GetBool("strict_mode", wasmCfg.StrictMode)
				}
			}

			// Create transport registry and register built-in transports
			transports := wasmapi.NewTransportRegistry()
			transports.Register(transport.NewWASIHTTPTransport())

			manager = component.NewManagerWithConfig(
				logger.Named("wasm.component"),
				bus,
				disp,
				fsReg,
				transports,
				wasmCfg,
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
