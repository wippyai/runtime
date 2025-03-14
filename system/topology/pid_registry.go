package topology

import (
	"sync"

	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/topology"
	"go.uber.org/zap"
)

// PIDRegistry provides Erlang-style name registration for PIDs.
// It is optimized for concurrent access.
type PIDRegistry struct {
	parent   topology.PIDRegistry
	nameToID sync.Map // Maps from name (string) to Target
	idToName sync.Map // Maps from Target to name (string) - for efficient removal by Target
	logger   *zap.Logger
}

// PIDRegistryConfig holds configuration for a PIDRegistry.
type PIDRegistryConfig struct {
	Parent topology.PIDRegistry
	Logger *zap.Logger
}

// NewPIDRegistry creates a new empty Target registry
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

// Register associates a name with a Target
// Returns error if name is already taken
func (r *PIDRegistry) Register(name string, pid pubsub.PID) error {
	// Store name → Target mapping
	r.nameToID.Store(name, pid)

	// Store reverse mapping for efficient removal by Target
	// Using a sync.Map to store a slice of names for each Target
	var names []string
	if existingNames, ok := r.idToName.Load(pid); ok {
		names = existingNames.([]string)
	}
	names = append(names, name)
	r.idToName.Store(pid, names)

	r.logger.Debug("registered name to Target mapping",
		zap.String("name", name),
		zap.String("pid", pid.String()))

	return nil
}

// Unregister removes a name registration
// Returns true if the name was registered and has been removed
func (r *PIDRegistry) Unregister(name string) bool {
	// Load the Target for this name
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

	// Remove from nameToID map
	r.nameToID.Delete(name)

	// Update the reverse mapping in idToName
	if namesVal, ok := r.idToName.Load(pid); ok {
		names := namesVal.([]string)
		// Filter out the name we're removing
		updatedNames := make([]string, 0, len(names)-1)
		for _, n := range names {
			if n != name {
				updatedNames = append(updatedNames, n)
			}
		}

		// If there are still names, update the map
		// Otherwise, remove the Target entry entirely
		if len(updatedNames) > 0 {
			r.idToName.Store(pid, updatedNames)
		} else {
			r.idToName.Delete(pid)
		}
	}

	r.logger.Debug("unregistered name",
		zap.String("name", name),
		zap.String("pid", pid.String()))

	return true
}

// Lookup finds the Target registered with a given name
// Returns the Target and true if found, empty Target and false if not found
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

// Remove completely removes a Target from the registry,
// removing all name associations for that Target
func (r *PIDRegistry) Remove(pid pubsub.PID) {
	// Get all names associated with this Target
	namesVal, exists := r.idToName.Load(pid)
	if !exists {
		// If no names found in this registry, try parent
		if r.parent != nil {
			r.parent.Remove(pid)
		}
		return
	}

	names := namesVal.([]string)

	// Remove each name from the nameToID map
	for _, name := range names {
		r.nameToID.Delete(name)

		r.logger.Debug("removed name during Target removal",
			zap.String("name", name),
			zap.String("pid", pid.String()))
	}

	// Remove the Target from the idToName map
	r.idToName.Delete(pid)

	r.logger.Debug("removed Target from registry",
		zap.String("pid", pid.String()),
		zap.Int("names_removed", len(names)))

	// Propagate to parent if exists
	if r.parent != nil {
		r.parent.Remove(pid)
	}
}

// Ensure Registry implements the operation.Registry interface
var _ topology.PIDRegistry = (*PIDRegistry)(nil)
