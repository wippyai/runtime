// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const UDPCreateSocketNamespace = "wasi:sockets/udp-create-socket@0.2.0"

// UDPCreateSocketHost implements wasi:sockets/udp-create-socket@0.2.0.
type UDPCreateSocketHost struct {
	resources *preview2.ResourceTable
}

func NewUDPCreateSocketHost(resources *preview2.ResourceTable) *UDPCreateSocketHost {
	return &UDPCreateSocketHost{resources: resources}
}

func (h *UDPCreateSocketHost) Namespace() string {
	return UDPCreateSocketNamespace
}

func (h *UDPCreateSocketHost) CreateUDPSocket(_ context.Context, addressFamily uint8) (uint32, *NetworkError) {
	if addressFamily != AddressFamilyIPv4 && addressFamily != AddressFamilyIPv6 {
		return 0, &NetworkError{Code: NetworkErrorInvalidArgument}
	}

	socket := preview2.NewUDPSocketResource(addressFamily)
	handle := h.resources.Add(socket)
	return handle, nil
}

func (h *UDPCreateSocketHost) Register() map[string]any {
	return map[string]any{
		"create-udp-socket": h.CreateUDPSocket,
	}
}
