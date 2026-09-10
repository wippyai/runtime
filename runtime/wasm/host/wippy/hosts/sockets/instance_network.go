// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const InstanceNetworkNamespace = "wasi:sockets/instance-network@0.2.8"

// InstanceNetworkHost implements wasi:sockets/instance-network@0.2.8.
type InstanceNetworkHost struct {
	resources *preview2.ResourceTable
}

func NewInstanceNetworkHost(resources *preview2.ResourceTable) *InstanceNetworkHost {
	return &InstanceNetworkHost{resources: resources}
}

func (h *InstanceNetworkHost) Namespace() string {
	return InstanceNetworkNamespace
}

// InstanceNetwork creates a network resource handle.
func (h *InstanceNetworkHost) InstanceNetwork(_ context.Context) uint32 {
	network := preview2.NewNetworkResource()
	return h.resources.Add(network)
}

func (h *InstanceNetworkHost) Register() map[string]any {
	return map[string]any{
		"instance-network": h.InstanceNetwork,
	}
}
