package wasm

import (
	wippycli "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/cli"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var cliAliases = []string{
	wippycli.EnvironmentNamespace,
	wippycli.ExitNamespace,
	"wasi:cli/environment",
	"wasi:cli/exit",
}

func cliHosts(_ *preview2.ResourceTable) []wasmrt.Host {
	return []wasmrt.Host{
		wippycli.NewEnvironmentHost(),
		wippycli.NewExitHost(),
	}
}
