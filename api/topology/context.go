// SPDX-License-Identifier: MPL-2.0

// Package topology provides process communication and lifecycle management.
package topology

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var (
	topologyKey    = &ctxapi.Key{Name: "topology.topology"}
	registryKey    = &ctxapi.Key{Name: "topology.registry"}
	globalRegKey   = &ctxapi.Key{Name: "topology.global_registry"}
	eventualRegKey = &ctxapi.Key{Name: "topology.eventual_registry"}
)

// WithRegistry attaches a Target registry to the provided context.
// This allows the registry to be retrieved later using the GetRegistry function.
func WithRegistry(ctx context.Context, registry PIDRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(registryKey) == nil {
		ac.With(registryKey, registry)
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
	if val := ac.Get(registryKey); val != nil {
		if reg, ok := val.(PIDRegistry); ok {
			return reg
		}
	}
	return nil
}

// WithGlobalRegistry attaches a GlobalRegistry to the provided context.
func WithGlobalRegistry(ctx context.Context, reg GlobalRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(globalRegKey) == nil {
		ac.With(globalRegKey, reg)
	}
	return ctx
}

// GetGlobalRegistry retrieves the GlobalRegistry from the provided context.
// Returns nil if no global registry is found.
func GetGlobalRegistry(ctx context.Context) GlobalRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(globalRegKey); val != nil {
		if reg, ok := val.(GlobalRegistry); ok {
			return reg
		}
	}
	return nil
}

// WithEventualRegistry attaches an EventualRegistry to the provided context.
func WithEventualRegistry(ctx context.Context, reg EventualRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(eventualRegKey) == nil {
		ac.With(eventualRegKey, reg)
	}
	return ctx
}

// GetEventualRegistry retrieves the EventualRegistry from the provided context.
// Returns nil if no eventual registry is found.
func GetEventualRegistry(ctx context.Context) EventualRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(eventualRegKey); val != nil {
		if reg, ok := val.(EventualRegistry); ok {
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
	if ac.Get(topologyKey) == nil {
		ac.With(topologyKey, topology)
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
	if val := ac.Get(topologyKey); val != nil {
		if top, ok := val.(Topology); ok {
			return top
		}
	}
	return nil
}
