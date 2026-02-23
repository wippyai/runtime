// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
	"go.uber.org/zap"
)

// hostFactory creates hosts backed by a shared resource table.
type hostFactory func(resources *preview2.ResourceTable) []wasmrt.Host

// DefaultHostProfiles returns the default WASM host profiles wired by boot.
func DefaultHostProfiles(log *zap.Logger, disp dispatcher.Dispatcher) []wasmcomponent.HostProfile {
	return []wasmcomponent.HostProfile{
		funcsHostProfile(log),
		wasi1HostProfile(),
		wasiIOProfile(log),
		wasiPollProfile(log),
		wasiClocksProfile(log),
		wasiCLIProfile(log),
		wasiFilesystemProfile(log),
		wasiRandomProfile(log),
		wasiSocketsProfile(log),
		wasiHTTPProfile(disp, log),
	}
}

// getSharedResources retrieves or creates the shared ResourceTable from the HostRegistry in context.
func getSharedResources(ctx context.Context) *preview2.ResourceTable {
	reg := wasmcomponent.GetHostRegistry(ctx)
	if reg == nil {
		return preview2.NewResourceTable()
	}
	if res := reg.SharedResources(); res != nil {
		return res.(*preview2.ResourceTable)
	}
	rt := preview2.NewResourceTable()
	reg.SetSharedResources(rt)
	return rt
}

func registerHosts(ctx context.Context, rt *wasmrt.Runtime, factories []hostFactory, log *zap.Logger, profileName string) error {
	resources := getSharedResources(ctx)
	for _, factory := range factories {
		for _, host := range factory(resources) {
			if err := rt.RegisterHost(host); err != nil {
				return runtimewasm.NewRegisterHostError(host.Namespace(), err)
			}
		}
	}
	if log != nil {
		log.Info("wasm host profile registered", zap.String("profile", profileName))
	}
	return nil
}

func wasiIOProfile(log *zap.Logger) wasmcomponent.HostProfile {
	return wasmcomponent.HostProfile{
		Name:          wasmcomponent.HostProfileWASIIO,
		ComponentOnly: true,
		Aliases:       append([]string{wasmcomponent.HostProfileWASIIO}, ioAliases...),
		Register: func(ctx context.Context, rt *wasmrt.Runtime) error {
			return registerHosts(ctx, rt, []hostFactory{ioHosts}, log, wasmcomponent.HostProfileWASIIO)
		},
	}
}

func wasiPollProfile(log *zap.Logger) wasmcomponent.HostProfile {
	return wasmcomponent.HostProfile{
		Name:          wasmcomponent.HostProfileWASIPoll,
		ComponentOnly: true,
		Aliases:       append([]string{wasmcomponent.HostProfileWASIPoll}, pollAliases...),
		Register: func(ctx context.Context, rt *wasmrt.Runtime) error {
			return registerHosts(ctx, rt, []hostFactory{pollHosts}, log, wasmcomponent.HostProfileWASIPoll)
		},
	}
}

func wasiClocksProfile(log *zap.Logger) wasmcomponent.HostProfile {
	return wasmcomponent.HostProfile{
		Name:          wasmcomponent.HostProfileWASIClocks,
		ComponentOnly: true,
		Aliases:       append([]string{wasmcomponent.HostProfileWASIClocks}, clocksAliases...),
		Register: func(ctx context.Context, rt *wasmrt.Runtime) error {
			return registerHosts(ctx, rt, []hostFactory{clocksHosts}, log, wasmcomponent.HostProfileWASIClocks)
		},
	}
}

func wasiCLIProfile(log *zap.Logger) wasmcomponent.HostProfile {
	return wasmcomponent.HostProfile{
		Name:          wasmcomponent.HostProfileWASICLI,
		ComponentOnly: true,
		Aliases:       append([]string{wasmcomponent.HostProfileWASICLI}, cliAliases...),
		Register: func(ctx context.Context, rt *wasmrt.Runtime) error {
			return registerHosts(ctx, rt, []hostFactory{cliHosts}, log, wasmcomponent.HostProfileWASICLI)
		},
	}
}

func wasiFilesystemProfile(log *zap.Logger) wasmcomponent.HostProfile {
	return wasmcomponent.HostProfile{
		Name:          wasmcomponent.HostProfileWASIFilesystem,
		ComponentOnly: true,
		Aliases:       append([]string{wasmcomponent.HostProfileWASIFilesystem}, filesystemAliases...),
		Register: func(ctx context.Context, rt *wasmrt.Runtime) error {
			return registerHosts(ctx, rt, []hostFactory{filesystemHosts}, log, wasmcomponent.HostProfileWASIFilesystem)
		},
	}
}

func wasiRandomProfile(log *zap.Logger) wasmcomponent.HostProfile {
	return wasmcomponent.HostProfile{
		Name:          wasmcomponent.HostProfileWASIRandom,
		ComponentOnly: true,
		Aliases:       append([]string{wasmcomponent.HostProfileWASIRandom}, randomAliases...),
		Register: func(ctx context.Context, rt *wasmrt.Runtime) error {
			return registerHosts(ctx, rt, []hostFactory{randomHosts}, log, wasmcomponent.HostProfileWASIRandom)
		},
	}
}

func wasiSocketsProfile(log *zap.Logger) wasmcomponent.HostProfile {
	return wasmcomponent.HostProfile{
		Name:          wasmcomponent.HostProfileWASISockets,
		ComponentOnly: true,
		Aliases:       append([]string{wasmcomponent.HostProfileWASISockets}, socketsAliases...),
		Register: func(ctx context.Context, rt *wasmrt.Runtime) error {
			return registerHosts(ctx, rt, []hostFactory{socketsHosts}, log, wasmcomponent.HostProfileWASISockets)
		},
	}
}

func wasiHTTPProfile(d dispatcher.Dispatcher, log *zap.Logger) wasmcomponent.HostProfile {
	return wasmcomponent.HostProfile{
		Name:          wasmcomponent.HostProfileWASIHTTP,
		ComponentOnly: true,
		Aliases:       append([]string{wasmcomponent.HostProfileWASIHTTP}, httpAliases...),
		Register: func(ctx context.Context, rt *wasmrt.Runtime) error {
			if d == nil {
				return runtimewasm.ErrDispatcherNotFound
			}
			return registerHosts(ctx, rt, []hostFactory{httpHosts}, log, wasmcomponent.HostProfileWASIHTTP)
		},
	}
}
