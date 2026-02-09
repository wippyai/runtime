package wasm

import (
	wippyrandom "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/random"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var randomAliases = []string{
	wippyrandom.SecureNamespace,
	wippyrandom.InsecureNamespace,
	wippyrandom.InsecureSeedNamespace,
	"wasi:random/random",
	"wasi:random/insecure",
	"wasi:random/insecure-seed",
}

func randomHosts(_ *preview2.ResourceTable) []wasmrt.Host {
	return []wasmrt.Host{
		wippyrandom.NewSecureRandomHost(),
		wippyrandom.NewInsecureRandomHost(),
		wippyrandom.NewInsecureSeedHost(),
	}
}
