package boot

import "context"

// Phase represents a stage in the application boot lifecycle.
type Phase int

const (
	// PreInit creates core infrastructure (EventBus, Logger, PIDGen, AppContext).
	PreInit Phase = iota

	// Init creates registries and system services.
	Init

	// PostInit creates service managers and handlers.
	PostInit

	// Start activates services after context is finalized.
	Start
)

// String returns the phase name.
func (p Phase) String() string {
	switch p {
	case PreInit:
		return "PreInit"
	case Init:
		return "Init"
	case PostInit:
		return "PostInit"
	case Start:
		return "Start"
	default:
		return "Unknown"
	}
}

// Component represents a component loaded during application boot.
type Component interface {
	// Name returns unique component identifier.
	Name() string

	// Phase returns when this component should load.
	Phase() Phase

	// DependsOn returns names of components that must load before this one.
	// Return nil or empty slice for no dependencies.
	DependsOn() []string

	// Load creates the service and attaches it to context.
	// Returns error if component failed to load.
	Load(ctx context.Context) (context.Context, error)
}

// Starter is implemented by components that need activation after Load.
type Starter interface {
	// Start activates the service (listeners, background tasks, etc).
	Start(ctx context.Context) error
}

// Stopper is implemented by components that need graceful shutdown.
type Stopper interface {
	// Stop gracefully shuts down the service.
	Stop(ctx context.Context) error
}
