// SPDX-License-Identifier: MPL-2.0

package registry

import "context"

// Scope indicates whether an operation should be persisted to history.
type Scope int

const (
	// ScopeHistory operations are saved to history and advance versions.
	ScopeHistory Scope = iota
	// ScopeBaseline operations are applied to state but not saved to history.
	ScopeBaseline
)

// ScopedOperation ties an operation to its persistence scope.
type ScopedOperation struct {
	Operation Operation
	Scope     Scope
}

// DirectiveResult is returned by a Directive to augment a registry operation.
type DirectiveResult struct {
	OriginalScope *Scope
	Resolution    *DependencyResolution
	Additional    []ScopedOperation
	Effects       []Effect
	Applied       bool
}

// ResolutionDirective restores derived state from the exact dependency graph
// stored for a registry version. It is used once after declarative history has
// been reconstructed, never once per historical changeset.
type ResolutionDirective interface {
	ReconcileResolution(context.Context, ProvenancedState, *DependencyResolution) (DirectiveResult, error)
}

// ResolutionTransitionDirective is the transition-aware form of
// ResolutionDirective. Current is the live state before the transition and
// target is the declarative target reconstructed from history. The registry
// prefers this interface when implemented and falls back to
// ResolutionDirective for compatibility.
type ResolutionTransitionDirective interface {
	ReconcileResolutionTransition(context.Context, ProvenancedState, ProvenancedState, *DependencyResolution) (DirectiveResult, error)
}

// ChangesDirective expands all same-kind operations in one resolution pass.
// It prevents a multi-root transaction from staging intermediate graphs.
type ChangesDirective interface {
	ExpandChanges(context.Context, ChangeSet, ProvenancedState) (DirectiveResult, error)
}

// Directive can augment a registry operation with additional operations or effects.
// Implementations may perform external work but must honor the provided context.
// Directives must not call Apply/ApplyVersion/LoadState (Apply is not re-entrant).
// Use Effects for work that must be staged, committed, or rolled back alongside Apply.
type Directive interface {
	Expand(ctx context.Context, op Operation, snapshot ProvenancedState) (DirectiveResult, error)
}

// Effect represents external work tied to an expanded operation.
// Prepare should stage resources, Commit finalizes them, Rollback reverts them.
// Effects must not call Apply/ApplyVersion/LoadState (Apply is not re-entrant).
type Effect interface {
	Prepare(context.Context) error
	Commit(context.Context) error
	Rollback(context.Context) error
}

// FinalizingEffect performs irreversible cleanup only after the registry state
// and its history head are durably committed. Finalize must not be required for
// correctness: a failure may leak temporary resources, but must not invalidate
// the committed registry state.
type FinalizingEffect interface {
	Effect
	Finalize(context.Context) error
}
