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

type (
	// Registry provides cluster-wide name registration with strong consistency.
	// All write operations go through Raft; reads are served from the local replica.
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

		// Lookup reads the name from the local Raft replica.
		// May return slightly stale data (eventual consistency for reads).
		Lookup(name string) (pid.PID, bool)

		// LookupByPID returns all global names registered to a PID.
		LookupByPID(p pid.PID) []string

		// Remove removes all global names for a PID. Goes through Raft.
		Remove(ctx context.Context, p pid.PID) error

		// RemoveNode removes all global names owned by processes on the given node.
		// Typically called when a node leaves the cluster.
		RemoveNode(ctx context.Context, nodeID pid.NodeID) error
	}
)

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
