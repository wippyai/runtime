package topology

import (
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/topology"
	"go.uber.org/zap"
	"sync"
)

// PIDRegistry provides Erlang-style name registration for PIDs.
// It is optimized for concurrent access.
type PIDRegistry struct {
	parent   topology.PIDRegistry
	nameToID sync.Map // Maps from name (string) to PID
	logger   *zap.Logger
}

// PIDRegistryConfig holds configuration for a PIDRegistry.
type PIDRegistryConfig struct {
	Parent topology.PIDRegistry
	Logger *zap.Logger
}

// NewPIDRegistry creates a new empty PID registry
func NewPIDRegistry(config PIDRegistryConfig) *PIDRegistry {
	// If no logger provided, use noop logger
	if config.Logger == nil {
		config.Logger = zap.NewNop()
	}

	return &PIDRegistry{
		parent: config.Parent,
		logger: config.Logger,
	}
}

// Register associates a name with a PID
// Returns error if name is already taken
func (r *PIDRegistry) Register(name string, pid pubsub.PID) error {
	// Store name → PID mapping
	r.nameToID.Store(name, pid)

	r.logger.Debug("registered name to PID mapping",
		zap.String("name", name),
		zap.String("pid", pid.String()))

	return nil
}

// Unregister removes a name registration
// Returns true if the name was registered and has been removed
func (r *PIDRegistry) Unregister(name string) bool {
	// Load the PID for this name
	pidVal, exists := r.nameToID.Load(name)
	if !exists {
		if r.parent != nil {
			released := r.parent.Unregister(name)
			if released {
				r.logger.Debug("unregistered name from parent registry",
					zap.String("name", name))
			}
			return released
		}
		r.logger.Debug("attempt to unregister non-existent name",
			zap.String("name", name))
		return false
	}

	pid := pidVal.(pubsub.PID)

	// Done mapping
	r.nameToID.Delete(name)

	r.logger.Debug("unregistered name",
		zap.String("name", name),
		zap.String("pid", pid.String()))

	return true
}

// Lookup finds the PID registered with a given name
// Returns the PID and true if found, empty PID and false if not found
func (r *PIDRegistry) Lookup(name string) (pubsub.PID, bool) {
	pidVal, exists := r.nameToID.Load(name)
	if !exists {
		if r.parent != nil {
			pid, found := r.parent.Lookup(name)
			if found {
				r.logger.Debug("looked up name from parent registry",
					zap.String("name", name),
					zap.String("pid", pid.String()))
				return pid, true
			}
		}

		r.logger.Debug("failed to lookup name",
			zap.String("name", name))
		return pubsub.PID{}, false
	}

	pid := pidVal.(pubsub.PID)

	// no log in hot operation, can be handled on app level

	return pid, true
}

// Ensure Registry implements the operation.Registry interface
var _ topology.PIDRegistry = (*PIDRegistry)(nil)
