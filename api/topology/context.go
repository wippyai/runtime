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

// WithRegistry attaches a Target registry to the provided context.
// This allows the registry to be retrieved later using the GetRegistry function.
func WithRegistry(ctx context.Context, registry PIDRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(registryCtx) == nil {
		ac.With(registryCtx, registry)
	}
	return ctx
}

// GetRegistry retrieves the Target registry from the provided context.
// Returns nil if no registry is found in the context.
func GetRegistry(ctx context.Context) PIDRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(registryCtx); val != nil {
		if reg, ok := val.(PIDRegistry); ok {
			return reg
		}
	}
	return nil
}

// WithTopology attaches a Topology instance to the provided context.
// This allows the topology to be retrieved later using the GetTopology function.
func WithTopology(ctx context.Context, topology Topology) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(topologyCtx) == nil {
		ac.With(topologyCtx, topology)
	}
	return ctx
}

// GetTopology retrieves the Topology instance from the provided context.
// Returns nil if no topology is found in the context.
func GetTopology(ctx context.Context) Topology {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(topologyCtx); val != nil {
		if top, ok := val.(Topology); ok {
			return top
		}
	}
	return nil
}
