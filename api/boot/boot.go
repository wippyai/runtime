// Package boot provides application boot and component loading.
package boot

import "context"

type (
	// Component represents a component loaded during application boot.
	Component interface {
		// Name returns unique component identifier.
		Name() string

		// DependsOn returns names of components that must load before this one.
		// Return nil or empty slice for no dependencies.
		DependsOn() []string

		// Load creates the service and attaches it to context.
		// Returns error if component failed to load.
		Load(ctx context.Context) (context.Context, error)
	}

	// Starter is implemented by components that need activation after Load.
	Starter interface {
		// Start activates the service (listeners, background tasks, etc).
		Start(ctx context.Context) error
	}

	// Stopper is implemented by components that need graceful shutdown.
	Stopper interface {
		// Stop gracefully shuts down the service.
		Stop(ctx context.Context) error
	}
)
