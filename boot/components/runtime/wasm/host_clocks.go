package wasm

import (
	wippyclocks "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/clocks"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var clocksAliases = []string{
	wippyclocks.WallClockNamespace,
	wippyclocks.MonotonicClockNamespace,
	"wasi:clocks/wall-clock",
	"wasi:clocks/monotonic-clock",
}

func clocksHosts(resources *preview2.ResourceTable) []wasmrt.Host {
	return []wasmrt.Host{
		wippyclocks.NewWallClockHost(),
		wippyclocks.NewMonotonicClockHost(resources),
	}
}
