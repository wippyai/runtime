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
		wasi2HostProfile(disp, log),
	}
}

// funcsHostProfile composes the wippy function-call host profile.
func funcsHostProfile(log *zap.Logger) wasmcomponent.HostProfile {
	aliases := []string{wasmcomponent.HostProfileFuncs}
	aliases = append(aliases, funcsAliases...)

	return wasmcomponent.HostProfile{
		Name:          wasmcomponent.HostProfileFuncs,
		ComponentOnly: true,
		Aliases:       aliases,
		Register: func(ctx context.Context, rt *wasmrt.Runtime) error {
			hosts, err := funcsHosts(ctx)
			if err != nil {
				return err
			}
			for _, host := range hosts {
				if err := rt.RegisterHost(host); err != nil {
					return runtimewasm.NewRegisterHostError(host.Namespace(), err)
				}
			}
			if log != nil {
				log.Info("wasm host profile registered", zap.String("profile", wasmcomponent.HostProfileFuncs))
			}
			return nil
		},
	}
}

// wasi2HostProfile composes all WASI2 host capabilities into a single profile.
// Hosts share a per-runtime resource table created during registration.
func wasi2HostProfile(d dispatcher.Dispatcher, log *zap.Logger) wasmcomponent.HostProfile {
	aliases := []string{
		wasmcomponent.HostProfileWASI2,
		"wasi-preview2",
		"preview2",
	}
	aliases = append(aliases, clocksAliases...)
	aliases = append(aliases, pollAliases...)
	aliases = append(aliases, ioAliases...)
	aliases = append(aliases, cliAliases...)
	aliases = append(aliases, stdioAliases...)
	aliases = append(aliases, filesystemAliases...)
	aliases = append(aliases, randomAliases...)
	aliases = append(aliases, socketsAliases...)
	aliases = append(aliases, httpAliases...)

	factories := []hostFactory{
		clocksHosts,
		pollHosts,
		ioHosts,
		cliHosts,
		stdioHosts,
		filesystemHosts,
		randomHosts,
		socketsHosts,
		httpHosts,
	}

	return wasmcomponent.HostProfile{
		Name:          wasmcomponent.HostProfileWASI2,
		ComponentOnly: true,
		Aliases:       aliases,
		Register: func(ctx context.Context, rt *wasmrt.Runtime) error {
			if d == nil {
				return runtimewasm.ErrDispatcherNotFound
			}

			resources := preview2.NewResourceTable()

			for _, factory := range factories {
				for _, host := range factory(resources) {
					if err := rt.RegisterHost(host); err != nil {
						return runtimewasm.NewRegisterHostError(host.Namespace(), err)
					}
				}
			}

			if log != nil {
				log.Info("wasm host profile registered", zap.String("profile", wasmcomponent.HostProfileWASI2))
			}
			return nil
		},
	}
}
