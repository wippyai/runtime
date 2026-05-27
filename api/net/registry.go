// SPDX-License-Identifier: MPL-2.0

package net

import "github.com/wippyai/runtime/api/registry"

// NetworkRegistry provides lookup of network overlay services by registry ID.
type NetworkRegistry interface {
	// GetNetwork returns the Service for the given network registry ID.
	// Returns ErrNetworkNotFound if no service is registered with that ID.
	GetNetwork(id registry.ID) (Service, error)

	// HasNetwork returns true if a network with the given ID is registered.
	HasNetwork(id registry.ID) bool

	// NetworkKind returns the registry kind of the network with the given ID.
	// Returns empty string if not found.
	NetworkKind(id registry.ID) registry.Kind
}
