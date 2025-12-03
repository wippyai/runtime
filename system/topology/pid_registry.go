package topology

import (
	"sync"

	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

// PIDRegistry provides Erlang-style name registration for PIDs.
// It is optimized for concurrent access.
type PIDRegistry struct {
	parent   topology.PIDRegistry
	nameToID sync.Map // Maps from name (string) to PID
	idToName sync.Map // Maps from PID string to *pidNames
	logger   *zap.Logger
}

// pidNames holds names for a PID with its own mutex for atomic updates.
type pidNames struct {
	mu    sync.Mutex
	names []string
}

// PIDRegistryOption configures a PIDRegistry.
type PIDRegistryOption func(*PIDRegistry)

// WithParent sets a parent registry for fallback lookups.
func WithParent(parent topology.PIDRegistry) PIDRegistryOption {
	return func(r *PIDRegistry) {
		r.parent = parent
	}
}

// WithLogger sets the logger for the registry.
func WithLogger(logger *zap.Logger) PIDRegistryOption {
	return func(r *PIDRegistry) {
		r.logger = logger
	}
}

// NewPIDRegistry creates a new empty PID registry.
func NewPIDRegistry(opts ...PIDRegistryOption) *PIDRegistry {
	r := &PIDRegistry{
		logger: zap.NewNop(),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Register associates a name with a PID.
// Overwrites if the name is already registered.
func (r *PIDRegistry) Register(name string, pid relay.PID) error {
	r.nameToID.Store(name, pid)

	// Get or create pidNames for this PID
	pidKey := pid.String()
	val, _ := r.idToName.LoadOrStore(pidKey, &pidNames{})
	pn := val.(*pidNames)

	pn.mu.Lock()
	pn.names = append(pn.names, name)
	pn.mu.Unlock()

	r.logger.Debug("registered name to PID mapping",
		zap.String("name", name),
		zap.String("pid", pidKey))

	return nil
}

// Unregister removes a name registration.
// Returns true if the name was registered and has been removed.
func (r *PIDRegistry) Unregister(name string) bool {
	pidVal, exists := r.nameToID.LoadAndDelete(name)
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

	pid := pidVal.(relay.PID)
	pidKey := pid.String()

	// Update the reverse mapping
	if val, ok := r.idToName.Load(pidKey); ok {
		pn := val.(*pidNames)
		pn.mu.Lock()
		updatedNames := make([]string, 0, len(pn.names)-1)
		for _, n := range pn.names {
			if n != name {
				updatedNames = append(updatedNames, n)
			}
		}
		pn.names = updatedNames
		empty := len(pn.names) == 0
		pn.mu.Unlock()

		if empty {
			r.idToName.Delete(pidKey)
		}
	}

	r.logger.Debug("unregistered name",
		zap.String("name", name),
		zap.String("pid", pidKey))

	return true
}

// Lookup finds the PID registered with a given name.
// Returns the PID and true if found, empty PID and false if not found.
func (r *PIDRegistry) Lookup(name string) (relay.PID, bool) {
	if pidVal, exists := r.nameToID.Load(name); exists {
		return pidVal.(relay.PID), true
	}

	if r.parent != nil {
		return r.parent.Lookup(name)
	}

	return relay.PID{}, false
}

// Remove completely removes a PID from the registry,
// removing all name associations for that PID.
func (r *PIDRegistry) Remove(pid relay.PID) {
	pidKey := pid.String()

	val, exists := r.idToName.LoadAndDelete(pidKey)
	if !exists {
		if r.parent != nil {
			r.parent.Remove(pid)
		}
		return
	}

	pn := val.(*pidNames)
	pn.mu.Lock()
	names := pn.names
	pn.names = nil
	pn.mu.Unlock()

	for _, name := range names {
		r.nameToID.Delete(name)
	}

	r.logger.Debug("removed PID from registry",
		zap.String("pid", pidKey),
		zap.Int("names_removed", len(names)))

	if r.parent != nil {
		r.parent.Remove(pid)
	}
}

// Ensure Registry implements the operation.Registry interface
var _ topology.PIDRegistry = (*PIDRegistry)(nil)
