// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	wippyio "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/io"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var ioAliases = []string{
	wippyio.ErrorNamespace,
	wippyio.StreamsNamespace,
	"wasi:io/error",
	"wasi:io/streams",
}

func ioHosts(resources *preview2.ResourceTable) []wasmrt.Host {
	return []wasmrt.Host{
		wippyio.NewErrorHost(resources),
		wippyio.NewStreamsHost(resources),
	}
}
