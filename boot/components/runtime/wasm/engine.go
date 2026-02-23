// SPDX-License-Identifier: MPL-2.0

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
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmfunc "github.com/wippyai/runtime/runtime/wasm/component/function"
	wasmproc "github.com/wippyai/runtime/runtime/wasm/component/process"
)

// Engine wires WASM function registry handling and runtime lifecycle.
func Engine() boot.Component {
	return EngineWithHostProfiles()
}

// EngineWithHostProfiles wires WASM function runtime using provided host profiles.
// This is the extension point for boot-time host plugins.
func EngineWithHostProfiles(hostProfiles ...wasmcomponent.HostProfile) boot.Component {
	profiles := append([]wasmcomponent.HostProfile(nil), hostProfiles...)
	var funcs *wasmfunc.Manager
	var procs *wasmproc.Manager

	return boot.New(boot.P{
		Name:      EngineName,
		DependsOn: []boot.Name{dispatchers.ClockDispatcherName, dispatchers.SocketDispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)
			transportRegistry, err := newTransportRegistry()
			if err != nil {
				return ctx, err
			}
			ctx = wasmapi.SetTransportRegistry(ctx, transportRegistry)

			disp := dispatcherapi.GetDispatcher(ctx)
			if disp == nil {
				return ctx, dispatchers.ErrDispatcherNotFound
			}

			fsReg := fsapi.GetRegistry(ctx)
			funcs = wasmfunc.NewManager(
				logger.Named("wasm.func"),
				bus,
				disp,
				fsReg,
			)
			procs = wasmproc.NewManager(
				logger.Named("wasm.process"),
				bus,
				fsReg,
			)
			effectiveProfiles := profiles
			if len(effectiveProfiles) == 0 {
				effectiveProfiles = DefaultHostProfiles(logger.Named("wasm.host"), disp)
			}
			if err := funcs.RegisterHostProfiles(effectiveProfiles...); err != nil {
				return ctx, err
			}
			if err := procs.RegisterHostProfiles(effectiveProfiles...); err != nil {
				return ctx, err
			}

			handlers.Register(wasmcomponent.NewHandler("function.(wasm|wat)", funcs))
			handlers.Register(wasmcomponent.NewHandler("process.wasm", procs))

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if funcs != nil {
				if err := funcs.Start(ctx); err != nil {
					return err
				}
			}
			if procs != nil {
				if err := procs.Start(ctx); err != nil {
					return err
				}
			}
			return nil
		},
		Stop: func(_ context.Context) error {
			if funcs != nil {
				funcs.Stop()
			}
			if procs != nil {
				procs.Stop()
			}
			return nil
		},
	})
}
