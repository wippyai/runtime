package wasm

import (
	wippysockets "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/sockets"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var socketsAliases = []string{
	wippysockets.InstanceNetworkNamespace,
	wippysockets.TCPCreateSocketNamespace,
	wippysockets.TCPNamespace,
	wippysockets.UDPCreateSocketNamespace,
	wippysockets.UDPNamespace,
	wippysockets.IPNameLookupNamespace,
	"wasi:sockets/instance-network",
	"wasi:sockets/tcp-create-socket",
	"wasi:sockets/tcp",
	"wasi:sockets/udp-create-socket",
	"wasi:sockets/udp",
	"wasi:sockets/ip-name-lookup",
}

func socketsHosts(resources *preview2.ResourceTable) []wasmrt.Host {
	return []wasmrt.Host{
		wippysockets.NewInstanceNetworkHost(resources),
		wippysockets.NewTCPCreateSocketHost(resources),
		wippysockets.NewTCPHost(resources),
		wippysockets.NewUDPCreateSocketHost(resources),
		wippysockets.NewUDPHost(resources),
		wippysockets.NewIPNameLookupHost(resources),
	}
}
