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
	Additional    []ScopedOperation
	Effects       []Effect
	Applied       bool
}

// Directive can augment a registry operation with additional operations or effects.
// Implementations may perform external work but must honor the provided context.
// Directives must not call Apply/ApplyVersion/LoadState (Apply is not re-entrant).
// Use Effects for work that must be staged, committed, or rolled back alongside Apply.
type Directive interface {
	Expand(ctx context.Context, op Operation, snapshot State) (DirectiveResult, error)
}

// Effect represents external work tied to an expanded operation.
// Prepare should stage resources, Commit finalizes them, Rollback reverts them.
// Effects must not call Apply/ApplyVersion/LoadState (Apply is not re-entrant).
type Effect interface {
	Prepare(context.Context) error
	Commit(context.Context) error
	Rollback(context.Context) error
}
