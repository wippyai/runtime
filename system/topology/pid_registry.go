// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

// PIDRegistry provides Erlang-style name registration for PIDs.
// It is optimized for concurrent access.
type PIDRegistry struct {
	parent      topology.PIDRegistry
	globalReg   atomic.Value // stores topology.GlobalRegistry
	eventualReg atomic.Value // stores topology.EventualRegistry
	logger      *zap.Logger
	nameToID    sync.Map
	idToName    sync.Map
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

// WithEventualRegistry sets an eventual (gossip-based) registry for
// cross-scope conflict checking. A name registered as Eventual blocks
// the same name from being registered as Local on this node.
func WithEventualRegistry(eventual topology.EventualRegistry) Option {
	return func(r *PIDRegistry) {
		r.eventualReg.Store(eventual)
	}
}

// SetGlobalRegistry sets the global registry after construction.
// This is needed when the global registry is initialized after the PID registry.
// Safe for concurrent use.
func (r *PIDRegistry) SetGlobalRegistry(global topology.GlobalRegistry) {
	r.globalReg.Store(global)
}

// SetEventualRegistry sets the eventual registry after construction.
// Safe for concurrent use.
func (r *PIDRegistry) SetEventualRegistry(eventual topology.EventualRegistry) {
	r.eventualReg.Store(eventual)
}

// loadGlobalReg returns the current global registry, or nil if none is set.
func (r *PIDRegistry) loadGlobalReg() topology.GlobalRegistry {
	v := r.globalReg.Load()
	if v == nil {
		return nil
	}
	return v.(topology.GlobalRegistry)
}

// loadEventualReg returns the current eventual registry, or nil if none is set.
func (r *PIDRegistry) loadEventualReg() topology.EventualRegistry {
	v := r.eventualReg.Load()
	if v == nil {
		return nil
	}
	return v.(topology.EventualRegistry)
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
	// A held Strong reservation (a pending the node acked, awaiting promotion)
	// also blocks a conflicting local bind so the name cannot be granted to a
	// different pid during the promotion window.
	if gr := r.loadGlobalReg(); gr != nil {
		if res, err := gr.Lookup(context.Background(), name); err == nil && res.Found {
			if res.PID == p {
				return p, nil // same PID registered globally — allow
			}
			return res.PID, topology.ErrNameAlreadyRegistered
		}
		if reserved, ok := gr.IsStrongReserved(name); ok {
			if reserved == p {
				return p, nil // same PID reserved — allow
			}
			return reserved, topology.ErrNameAlreadyRegistered
		}
		// Join-epoch gate: refuse a fresh LOCAL bind while the barrier is in
		// progress. The node has not yet learned the cluster's Strong names and a
		// new bind could shadow one. A re-register of a name this node already
		// holds is allowed below (no shadowing risk) so the gate sits after the
		// existing-binding fast paths.
		if !gr.NameReady() {
			if existing, ok := r.nameToID.Load(name); ok {
				if ep, ok2 := existing.(pid.PID); ok2 && ep == p {
					return p, nil
				}
			}
			return p, topology.ErrNameServiceNotReady
		}
	}

	// Check eventual registry second to prevent local shadowing of eventual names.
	if er := r.loadEventualReg(); er != nil {
		if res, err := er.Lookup(context.Background(), name); err == nil && res.Found {
			if res.PID == p {
				return p, nil // same PID registered eventually — allow
			}
			return res.PID, topology.ErrNameAlreadyRegistered
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
	// Check global registry first — global names take priority (strongest consistency).
	if gr := r.loadGlobalReg(); gr != nil {
		if res, err := gr.Lookup(context.Background(), name); err == nil && res.Found {
			return res.PID, true
		}
	}

	// Check eventual registry second — gossip-replicated names.
	if er := r.loadEventualReg(); er != nil {
		if res, err := er.Lookup(context.Background(), name); err == nil && res.Found {
			return res.PID, true
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

// LookupLocal reads only this registry's own name table, bypassing the global
// and eventual cross-scope registries. It exists so globalreg's conditional ack
// can attest LOCAL-scope non-presence without re-entering globalreg (which
// would self-reference a held Strong reservation). Does not consult the parent.
func (r *PIDRegistry) LookupLocal(name string) (pid.PID, bool) {
	if pidVal, exists := r.nameToID.Load(name); exists {
		if p, ok := pidVal.(pid.PID); ok {
			return p, true
		}
	}
	return pid.PID{}, false
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
