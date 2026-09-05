// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const NetworkNamespace = "wasi:sockets/network@0.2.8"

// NetworkHost implements wasi:sockets/network@0.2.8.
type NetworkHost struct {
	resources *preview2.ResourceTable
}

func NewNetworkHost(resources *preview2.ResourceTable) *NetworkHost {
	return &NetworkHost{resources: resources}
}

func (h *NetworkHost) Namespace() string {
	return NetworkNamespace
}

// ResourceDropNetwork releases a network resource handle.
func (h *NetworkHost) ResourceDropNetwork(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

func (h *NetworkHost) Register() map[string]any {
	return map[string]any{
		"[resource-drop]network": h.ResourceDropNetwork,
	}
}
