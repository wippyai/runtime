// SPDX-License-Identifier: MPL-2.0

// Package globalreg provides the API for the distributed global name registry.
// Global names are unique across the entire cluster, backed by Raft consensus.
package global

import (
	"context"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
)

// RegistrationMode controls whether a name is registered locally or globally.
// Wire values match topology.RegistrationMode so the Lua surface and the Go
// surface share one constant family.
type RegistrationMode int

const (
	// Local is the default; the name is visible only on the registering node.
	Local RegistrationMode = 0
	// Eventual registers the name cluster-wide via gossip/CRDT (eventualreg).
	Eventual RegistrationMode = 1
	// Consistent registers the name cluster-wide via Raft as a linearizable
	// singleton.
	Consistent RegistrationMode = 2
	// Strong registers the name cluster-wide via Raft plus an all-live-node
	// ack on the committed epoch within a deadline. Strictest scope.
	Strong RegistrationMode = 3
)

// RegisterState describes the lifecycle stage of a Strong-scope reservation.
type RegisterState uint8

const (
	// RegisterStateUnknown is the zero value; never returned by a successful call.
	RegisterStateUnknown RegisterState = 0
	// RegisterStateActive means the registration is authoritative — every
	// live node in the snapshot acked the committed epoch.
	RegisterStateActive RegisterState = 1
	// RegisterStateExpired means the deadline elapsed before the ack set
	// was complete and the reservation was released.
	RegisterStateExpired RegisterState = 2
)

// RegisterOutcome is the public outcome of Register, regardless of scope.
type RegisterOutcome struct {
	// PID is the owner that won the registration. For Consistent this is
	// the supplied PID on success or the existing owner on conflict.
	PID pid.PID
	// ExistingPID is set on conflict (name already taken by a different PID).
	ExistingPID pid.PID
	// Epoch is the Raft log index that established authoritativeness
	// (Active for Strong; first-write index for Consistent).
	Epoch uint64
	// State is meaningful for Strong; for Consistent it is always
	// RegisterStateActive on success.
	State RegisterState
}

// StrongDeadline is the default ack deadline used when a caller does not
// supply one via context. Picked to give a 3-node loopback cluster plenty
// of margin while still surfacing real partitions quickly on chaos rigs.
// Exposed as a var so deterministic unit tests can shrink it.
var StrongDeadline = 10 * time.Second

// LookupOptions controls the behavior of Registry.Lookup. Build via the
// functional options (ByPID, IncludeStale). The zero value yields the
// cheapest read: only Found and PID are populated.
type LookupOptions struct {
	// ByPID, when non-nil, reverses the lookup: the registry returns all
	// names currently registered to this PID. The name argument to Lookup
	// is ignored when ByPID is set.
	ByPID *pid.PID
	// IncludeStale is a forward-looking flag reserved for future scope
	// support (e.g. surfacing pending Strong-scope registrations). Today
	// the registry returns only committed-active entries regardless of
	// this flag; readers that explicitly request stale entries get the
	// same result.
	IncludeStale bool
}

// LookupOption mutates a LookupOptions struct.
type LookupOption func(*LookupOptions)

// ByPID reverses the lookup: returns all names registered to the given PID.
// The name argument is ignored.
func ByPID(p pid.PID) LookupOption {
	return func(o *LookupOptions) { o.ByPID = &p }
}

// IncludeStale is reserved for future scope support. See LookupOptions.
func IncludeStale() LookupOption {
	return func(o *LookupOptions) { o.IncludeStale = true }
}

// LookupResult holds the result of a global name lookup. Only fields that
// were requested via LookupOption are guaranteed to be populated.
//
//   - Found      — always present.
//   - PID        — populated when Found is true.
//   - NamesForPID — populated only when ByPID(p) was passed.
type LookupResult struct {
	PID         pid.PID
	NamesForPID []string
	Found       bool
}

// ResolveFunc is called when a name conflict is detected (e.g., after a
// partition heals and two nodes registered the same name independently).
// It receives the name, the existing owner, and the incoming claimant.
// It must return the PID that should keep the name. The loser receives
// a NameConflict notification via topology.
type ResolveFunc func(name string, existing, incoming pid.PID) pid.PID

// DefaultResolve keeps the existing registration (first-write-wins).
func DefaultResolve(_ string, existing, _ pid.PID) pid.PID {
	return existing
}

type (
	// Registry provides cluster-wide name registration with strong consistency.
	// All write operations go through Raft; reads are served from the local replica.
	Registry interface {
		// Register associates a name with a PID at scope Consistent:
		// Raft-committed singleton. Retained as a convenience; new callers
		// should use RegisterScope.
		Register(ctx context.Context, name string, p pid.PID) (pid.PID, error)

		// RegisterScope is the scope-aware Register. Behavior depends on mode:
		//
		//   Consistent — Raft linearizable singleton.
		//   Strong     — Raft singleton plus all-live-node ack on the
		//                committed epoch. Blocks until the FSM commits
		//                Active or Expired (or ctx is canceled).
		//   Eventual   — gossip/CRDT (routed by callers to eventualreg).
		//   Local      — caller error at this layer (use PIDRegistry).
		//
		// On Strong timeout, the returned error wraps
		// ErrStrongRegistrationTimeout via *StrongRegistrationTimeoutError so
		// callers can read MissingAcks via errors.As.
		RegisterScope(ctx context.Context, name string, p pid.PID, mode RegistrationMode) (RegisterOutcome, error)

		// Unregister removes the Consistent-scope registration for a name.
		Unregister(ctx context.Context, name string) (bool, error)

		// UnregisterScope removes the registration for the given scope.
		// Strong-scope unregister clears either a pending reservation or an
		// active registration, whichever exists.
		UnregisterScope(ctx context.Context, name string, mode RegistrationMode) (bool, error)

		// Lookup reads the registry from the local Raft FSM replica. The
		// fields populated in the returned LookupResult are determined by
		// the options:
		//
		//   no options       — Found + PID only (cheapest read).
		//   ByPID(p)         — ignores name; populates NamesForPID.
		//
		// Lookup never blocks on Raft; it may return slightly stale data
		// (eventual consistency for reads). Errors are reserved for
		// readiness/transport failures.
		Lookup(ctx context.Context, name string, opts ...LookupOption) (LookupResult, error)

		// Remove removes all global names for a PID. Goes through Raft.
		Remove(ctx context.Context, p pid.PID) error

		// RemoveNode removes all global names owned by processes on the given node.
		// Typically called when a node leaves the cluster.
		RemoveNode(ctx context.Context, nodeID pid.NodeID) error
	}
)

var globalRegKey = &ctxapi.Key{Name: "global.registry"}

// WithRegistry attaches a global Registry to the provided context.
func WithRegistry(ctx context.Context, reg Registry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(globalRegKey) == nil {
		ac.With(globalRegKey, reg)
	}
	return ctx
}

// GetRegistry retrieves the global Registry from the provided context.
// Returns nil if no global registry is found.
func GetRegistry(ctx context.Context) Registry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(globalRegKey); val != nil {
		if reg, ok := val.(Registry); ok {
			return reg
		}
	}
	return nil
}
