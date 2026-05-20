// SPDX-License-Identifier: MPL-2.0

// Package globalreg provides the API for the distributed global name registry.
// Global names are unique across the entire cluster, backed by Raft consensus.
package globalreg

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
)

// RegistrationMode controls whether a name is registered locally or globally.
type RegistrationMode int

const (
	// Local is the default; the name is visible only on the registering node.
	Local RegistrationMode = 0
	// Global registers the name cluster-wide via Raft consensus.
	Global RegistrationMode = 1
)

// LookupOptions controls the behavior of Registry.Lookup. Build via the
// functional options (WithFence, ByPID, IncludeStale). The zero value yields
// the cheapest read: only Found and PID are populated.
type LookupOptions struct {
	// ByPID, when non-nil, reverses the lookup: the registry returns all
	// names currently registered to this PID. The name argument to Lookup
	// is ignored when ByPID is set.
	ByPID *pid.PID
	// WithFence requests that the FenceToken field of the result be
	// populated with the Raft log index at which the name was registered.
	WithFence bool
	// IncludeStale is a forward-looking flag reserved for future scope
	// support (e.g. surfacing pending ROOT-scope registrations). Today
	// the registry returns only committed-active entries regardless of
	// this flag; readers that explicitly request stale entries get the
	// same result.
	IncludeStale bool
}

// LookupOption mutates a LookupOptions struct.
type LookupOption func(*LookupOptions)

// WithFence requests the FenceToken (Raft log index at registration time)
// be returned alongside the PID.
func WithFence() LookupOption {
	return func(o *LookupOptions) { o.WithFence = true }
}

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
//   - FenceToken — populated only when WithFence() was passed.
//   - NamesForPID — populated only when ByPID(p) was passed.
type LookupResult struct {
	PID         pid.PID
	NamesForPID []string
	FenceToken  uint64
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
	// Fencing tokens protect against stale references between Raft majority-commit
	// and full replication to all followers.
	Registry interface {
		// Register associates a name with a PID globally. The operation is
		// linearizable: it is guaranteed that at most one PID owns the name
		// at any point in time across the entire cluster.
		// Returns (p, nil) on success.
		// Returns (existingPID, ErrNameAlreadyRegistered) if taken by another PID.
		// Re-registering the same name+PID is idempotent.
		Register(ctx context.Context, name string, p pid.PID) (pid.PID, error)

		// Unregister removes a global name registration. Goes through Raft.
		Unregister(ctx context.Context, name string) (bool, error)

		// Lookup reads the registry from the local Raft FSM replica. The
		// fields populated in the returned LookupResult are determined by
		// the options:
		//
		//   no options       — Found + PID only (cheapest read).
		//   WithFence()      — also populates FenceToken.
		//   ByPID(p)         — ignores name; populates NamesForPID.
		//
		// Lookup never blocks on Raft; it may return slightly stale data
		// (eventual consistency for reads). Errors are reserved for
		// readiness/transport failures.
		Lookup(ctx context.Context, name string, opts ...LookupOption) (LookupResult, error)

		// Deprecated: use Lookup(ctx, name, WithFence()) instead. This
		// method is retained for one transition cycle so existing callers
		// keep compiling; the relay fence-validation hot path also calls
		// ValidateFence directly.
		LookupWithFence(name string) LookupResult

		// Deprecated: use Lookup(ctx, "", ByPID(p)) instead.
		LookupByPID(p pid.PID) []string

		// Deprecated: use globalreg.ValidateFence(ctx, reg, name, token).
		// Kept for the relay fence-validation hot path; will be removed
		// once T3 reworks the Lua surface.
		ValidateFence(name string, token uint64) error

		// Remove removes all global names for a PID. Goes through Raft.
		Remove(ctx context.Context, p pid.PID) error

		// RemoveNode removes all global names owned by processes on the given node.
		// Typically called when a node leaves the cluster.
		RemoveNode(ctx context.Context, nodeID pid.NodeID) error
	}
)

// ValidateFence is a one-line helper that asserts the supplied fencing
// token is still valid for name. It looks the name up via the unified
// Lookup with WithFence() and returns ErrStaleFence when the name no
// longer resolves or the token has been superseded.
func ValidateFence(ctx context.Context, reg Registry, name string, token uint64) error {
	r, err := reg.Lookup(ctx, name, WithFence())
	if err != nil {
		return err
	}
	if !r.Found || token < r.FenceToken {
		return ErrStaleFence
	}
	return nil
}

var globalRegKey = &ctxapi.Key{Name: "globalreg.registry"}

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
