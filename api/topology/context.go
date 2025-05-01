// Package topology provides process communication and lifecycle management.
package topology

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// Context keys for storing topology-related data
var (
	// topologyCtx is used to store the topology instance
	topologyCtx = &ctxapi.Key{Name: "topology.topology"}

	// registryCtx is used to store the Target registry
	registryCtx = &ctxapi.Key{Name: "topology.registry"}
)

// WithPIDRegistry attaches a Target registry to the provided context.
// This allows the registry to be retrieved later using the GetPIDRegistry function.
func WithPIDRegistry(ctx context.Context, registry PIDRegistry) context.Context {
	return context.WithValue(ctx, registryCtx, registry)
}

// GetPIDRegistry retrieves the Target registry from the provided context.
// Returns nil if no registry is found in the context.
func GetPIDRegistry(ctx context.Context) PIDRegistry {
	if reg, ok := ctx.Value(registryCtx).(PIDRegistry); ok {
		return reg
	}
	return nil
}

// WithTopology attaches a Topology instance to the provided context.
// This allows the topology to be retrieved later using the GetTopology function.
func WithTopology(ctx context.Context, topology Topology) context.Context {
	return context.WithValue(ctx, topologyCtx, topology)
}

// GetTopology retrieves the Topology instance from the provided context.
// Returns nil if no topology is found in the context.
func GetTopology(ctx context.Context) Topology {
	if top, ok := ctx.Value(topologyCtx).(Topology); ok {
		return top
	}
	return nil
}
