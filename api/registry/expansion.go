// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

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
	ReconcileResolution(context.Context, State, *DependencyResolution) (DirectiveResult, error)
}

// ResolutionTransitionDirective is the transition-aware form of
// ResolutionDirective. Current is the live state before the transition and
// target is the declarative target reconstructed from history. The registry
// prefers this interface when implemented and falls back to
// ResolutionDirective for compatibility.
type ResolutionTransitionDirective interface {
	ReconcileResolutionTransition(context.Context, State, State, *DependencyResolution) (DirectiveResult, error)
}

// ChangesDirective expands all same-kind operations in one resolution pass.
// It prevents a multi-root transaction from staging intermediate graphs.
type ChangesDirective interface {
	ExpandChanges(context.Context, ChangeSet, State) (DirectiveResult, error)
}

// Directive can augment a registry operation with additional operations or effects.
// Implementations may perform external work but must honor the provided context.
// Directives must not call Apply/ApplyVersion/LoadState (Apply is not re-entrant).
// Use Effects for work that must be staged, committed, or rolled back alongside Apply.
type Directive interface {
	Expand(ctx context.Context, op Operation, snapshot State) (DirectiveResult, error)
}

// EffectTarget identifies the external work an effect performs, independent of
// any staging identity, so a plan can be compared with a later apply.
type EffectTarget struct {
	// Kind names the class of work, such as "hub.artifact".
	Kind string
	// Digest is a lowercase sha256 hex over every input that changes what
	// Prepare or Commit does outside the registry.
	Digest string
}

// Plan is what an apply would do, computed without doing it.
type Plan struct {
	// Requested holds the operations the caller submitted.
	Requested ChangeSet
	// Changes holds every operation after directive expansion, in apply order.
	Changes ChangeSet
	// History is the subset of Changes that enters durable history.
	History ChangeSet
	// Base is the registry version the plan was computed against.
	Base Version
	// Digest binds Changes, History, Resolution and Effects.
	Digest string
	// Resolution is the exact module graph the apply would record.
	Resolution *DependencyResolution
	// Effects describes the external work the apply would perform.
	Effects []EffectTarget
}

// Planner computes a Plan through the same expansion Apply runs, then releases
// every staged resource. It never prepares, commits or finalizes an effect.
type Planner interface {
	Plan(context.Context, Version, ChangeSet) (*Plan, error)
}

// PlanApplier applies changes that were decided against a known version.
// ApplyAt refuses when the registry has moved past base. ApplyPlan also
// re-expands the requested operations and refuses when the plan digest no
// longer matches, so what was reviewed is what gets applied.
type PlanApplier interface {
	ApplyAt(context.Context, Version, ChangeSet) (Version, error)
	ApplyPlan(context.Context, *Plan) (Version, error)
}

// Effect represents external work tied to an expanded operation.
// Prepare should stage resources, Commit finalizes them, Rollback reverts them.
// Target describes the work so a plan can be verified against a later apply.
// Effects must not call Apply/ApplyVersion/LoadState (Apply is not re-entrant).
type Effect interface {
	Prepare(context.Context) error
	Commit(context.Context) error
	Rollback(context.Context) error
	Target() (EffectTarget, error)
}

// FinalizingEffect performs irreversible cleanup only after the registry state
// and its history head are durably committed. Finalize must not be required for
// correctness: a failure may leak temporary resources, but must not invalidate
// the committed registry state.
type FinalizingEffect interface {
	Effect
	Finalize(context.Context) error
}

// NewEffectTarget measures external work from a JSON-encodable description of
// its stable inputs. Callers exclude staging paths and other per-run identity.
func NewEffectTarget(kind string, value any) (EffectTarget, error) {
	encoded, err := json.Marshal(struct {
		Value any    `json:"value"`
		Kind  string `json:"kind"`
	}{Kind: kind, Value: value})
	if err != nil {
		return EffectTarget{}, err
	}
	sum := sha256.Sum256(encoded)
	return EffectTarget{Kind: kind, Digest: hex.EncodeToString(sum[:])}, nil
}
