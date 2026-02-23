// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	wippyhttp "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/http"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var httpAliases = []string{
	wippyhttp.TypesNamespace,
	wippyhttp.OutgoingHandlerNamespace,
	"wasi:http/types",
	"wasi:http/outgoing-handler",
}

func httpHosts(resources *preview2.ResourceTable) []wasmrt.Host {
	return []wasmrt.Host{
		wippyhttp.NewTypesHost(resources),
		wippyhttp.NewOutgoingHandlerHost(resources),
	}
}
