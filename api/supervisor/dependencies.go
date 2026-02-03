package supervisor

import "github.com/wippyai/runtime/api/registry"

// DependencyResolver is a function that returns additional dependencies for a given service ID.
// It allows the supervisor to discover implicit dependencies beyond those explicitly declared
// in the lifecycle configuration (e.g., dependencies extracted from registry entry data).
//
// The resolver receives a service ID and returns a list of dependency IDs that must be started
// before the given service. If the resolver returns an error, the dependency resolution fails
// and the service cannot be started.
//
// Example usage:
//
//	resolver := func(id registry.ID) ([]registry.ID, error) {
//	    entry := registry.Get(id)
//	    return topologyResolver.Extract(entry), nil
//	}
//	supervisor := New(bus, logger, WithDependencyResolver(resolver))
type DependencyResolver func(id registry.ID) ([]registry.ID, error)
