// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

// PIDRegistry provides Erlang-style name registration for PIDs.
// It is optimized for concurrent access.
type PIDRegistry struct {
	parent    topology.PIDRegistry
	globalReg atomic.Value // stores topology.GlobalRegistry
	logger    *zap.Logger
	nameToID  sync.Map
	idToName  sync.Map
}

// pidNames holds names for a PID with its own mutex for atomic updates.
type pidNames struct {
	names []string
	mu    sync.Mutex
}

// Option configures a PIDRegistry.
type Option func(*PIDRegistry)

// WithParent sets a parent registry for fallback lookups.
func WithParent(parent topology.PIDRegistry) Option {
	return func(r *PIDRegistry) {
		r.parent = parent
	}
}

// WithLogger sets the logger for the registry.
func WithLogger(logger *zap.Logger) Option {
	return func(r *PIDRegistry) {
		r.logger = logger
	}
}

// WithGlobalRegistry sets a global registry for cross-scope conflict checking.
func WithGlobalRegistry(global topology.GlobalRegistry) Option {
	return func(r *PIDRegistry) {
		r.globalReg.Store(global)
	}
}

// SetGlobalRegistry sets the global registry after construction.
// This is needed when the global registry is initialized after the PID registry.
// Safe for concurrent use.
func (r *PIDRegistry) SetGlobalRegistry(global topology.GlobalRegistry) {
	r.globalReg.Store(global)
}

// loadGlobalReg returns the current global registry, or nil if none is set.
func (r *PIDRegistry) loadGlobalReg() topology.GlobalRegistry {
	v := r.globalReg.Load()
	if v == nil {
		return nil
	}
	return v.(topology.GlobalRegistry)
}

// NewPIDRegistry creates a new empty PID registry.
func NewPIDRegistry(opts ...Option) *PIDRegistry {
	r := &PIDRegistry{
		logger: zap.NewNop(),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Register associates a name with a PID atomically.
// Returns (p, nil) on success.
// Returns (existingPID, ErrNameAlreadyRegistered) if name is taken by different PID.
// Re-registering same name with same PID is allowed and returns (p, nil).
// If a global registry is configured, local registration is rejected when
// the name already exists globally (prevents local shadowing of global names).
func (r *PIDRegistry) Register(name string, p pid.PID) (pid.PID, error) {
	// Check global registry first to prevent local shadowing of global names.
	if gr := r.loadGlobalReg(); gr != nil {
		if existingPID, found := gr.Lookup(name); found {
			if existingPID == p {
				return p, nil // same PID registered globally — allow
			}
			return existingPID, topology.ErrNameAlreadyRegistered
		}
	}

	actual, loaded := r.nameToID.LoadOrStore(name, p)
	if loaded {
		existingPID, ok := actual.(pid.PID)
		if !ok {
			return p, nil
		}
		// Name already exists - check if it's the same PID (re-registration is ok)
		if existingPID == p {
			return p, nil
		}
		return existingPID, topology.ErrNameAlreadyRegistered
	}

	pidKey := p.String()
	val, _ := r.idToName.LoadOrStore(pidKey, &pidNames{})
	pn, ok := val.(*pidNames)
	if !ok {
		return p, nil
	}

	pn.mu.Lock()
	pn.names = append(pn.names, name)
	pn.mu.Unlock()

	r.logger.Debug("registered name to PID mapping",
		zap.String("name", name),
		zap.String("pid", pidKey))

	return p, nil
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

	p, ok := pidVal.(pid.PID)
	if !ok {
		return false
	}
	pidKey := p.String()

	if val, ok := r.idToName.Load(pidKey); ok {
		pn, ok := val.(*pidNames)
		if !ok {
			return true
		}
		pn.mu.Lock()
		// Re-check if name was re-registered while waiting for the lock.
		// If it was, don't remove it from the names list.
		_, reregistered := r.nameToID.Load(name)
		if !reregistered {
			updatedNames := make([]string, 0, len(pn.names)-1)
			for _, n := range pn.names {
				if n != name {
					updatedNames = append(updatedNames, n)
				}
			}
			pn.names = updatedNames
		}
		empty := len(pn.names) == 0
		pn.mu.Unlock()

		if empty && !reregistered {
			r.idToName.Delete(pidKey)
		}
	}

	r.logger.Debug("unregistered name",
		zap.String("name", name),
		zap.String("pid", pidKey))

	return true
}

func (r *PIDRegistry) Lookup(name string) (pid.PID, bool) {
	// Check global registry first — global names take priority.
	if gr := r.loadGlobalReg(); gr != nil {
		if p, found := gr.Lookup(name); found {
			return p, true
		}
	}

	if pidVal, exists := r.nameToID.Load(name); exists {
		if p, ok := pidVal.(pid.PID); ok {
			return p, true
		}
	}

	if r.parent != nil {
		return r.parent.Lookup(name)
	}

	return pid.PID{}, false
}

// LookupWithFence looks up a global name and returns the fencing token.
// Returns a zero LookupResult if the name is not found globally (local names
// don't have fencing tokens).
func (r *PIDRegistry) LookupWithFence(name string) globalreg.LookupResult {
	if gr := r.loadGlobalReg(); gr != nil {
		result := gr.LookupWithFence(name)
		if result.Found {
			return result
		}
	}
	return globalreg.LookupResult{}
}

func (r *PIDRegistry) Remove(p pid.PID) {
	pidKey := p.String()

	val, exists := r.idToName.LoadAndDelete(pidKey)
	if !exists {
		if r.parent != nil {
			r.parent.Remove(p)
		}
		return
	}

	pn, ok := val.(*pidNames)
	if !ok {
		return
	}
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
		r.parent.Remove(p)
	}
}

// Ensure Registry implements the operation.Registry interface
var _ topology.PIDRegistry = (*PIDRegistry)(nil)
