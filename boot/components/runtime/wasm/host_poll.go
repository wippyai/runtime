package wasm

import (
	wippypoll "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/poll"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var pollAliases = []string{
	wippypoll.PollNamespace,
	"wasi:io/poll",
}

func pollHosts(resources *preview2.ResourceTable) []wasmrt.Host {
	return []wasmrt.Host{
		wippypoll.NewPollHost(resources),
	}
}
