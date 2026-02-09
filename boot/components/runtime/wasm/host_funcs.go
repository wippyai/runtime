package wasm

import (
	"context"

	functionapi "github.com/wippyai/runtime/api/function"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wippyfuncs "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/funcs"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

var funcsAliases = []string{
	wippyfuncs.FuncsNamespace,
}

func funcsHosts(ctx context.Context) ([]wasmrt.Host, error) {
	fnReg := functionapi.GetRegistry(ctx)
	if fnReg == nil {
		return nil, runtimewasm.ErrFunctionRegistryNotFound
	}
	return []wasmrt.Host{
		wippyfuncs.NewFuncsHost(fnReg),
	}, nil
}

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
