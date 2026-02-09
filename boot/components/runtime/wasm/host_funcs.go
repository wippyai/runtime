package wasm

import (
	"context"

	functionapi "github.com/wippyai/runtime/api/function"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	wippyfuncs "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/funcs"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
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
