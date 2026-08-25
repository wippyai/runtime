// SPDX-License-Identifier: MPL-2.0

package expansion

import (
	"context"

	"github.com/wippyai/runtime/api/registry"
)

// DependencyDirectiveFunc provides expansion for dependency entries.
// Implementations should honor the provided context.
type DependencyDirectiveFunc func(ctx context.Context, op registry.Operation, snapshot registry.ProvenancedState) (registry.DirectiveResult, error)
type DependencyChangesDirectiveFunc func(ctx context.Context, changes registry.ChangeSet, snapshot registry.ProvenancedState) (registry.DirectiveResult, error)
type ResolutionDirectiveFunc func(ctx context.Context, snapshot registry.ProvenancedState, resolution *registry.DependencyResolution) (registry.DirectiveResult, error)
type ResolutionTransitionDirectiveFunc func(ctx context.Context, current registry.ProvenancedState, target registry.ProvenancedState, resolution *registry.DependencyResolution) (registry.DirectiveResult, error)

// DependencyDirective expands dependency operations using a direct handler.
type DependencyDirective struct {
	ExpandFunc              DependencyDirectiveFunc
	ChangesFunc             DependencyChangesDirectiveFunc
	ReconcileFunc           ResolutionDirectiveFunc
	ReconcileTransitionFunc ResolutionTransitionDirectiveFunc
}

// NewDependencyDirective constructs a dependency directive backed by the given handler.
func NewDependencyDirective(expand DependencyDirectiveFunc, reconcile ...ResolutionDirectiveFunc) *DependencyDirective {
	directive := &DependencyDirective{ExpandFunc: expand}
	if len(reconcile) > 0 {
		directive.ReconcileFunc = reconcile[0]
	}
	return directive
}

// WithChangesExpansion makes a dependency transaction resolve its final root
// set once instead of resolving each intermediate operation.
func (d *DependencyDirective) WithChangesExpansion(expand DependencyChangesDirectiveFunc) *DependencyDirective {
	if d != nil {
		d.ChangesFunc = expand
	}
	return d
}

// WithResolutionTransition configures exact graph reconciliation with both the
// live source and declarative target state.
func (d *DependencyDirective) WithResolutionTransition(reconcile ResolutionTransitionDirectiveFunc) *DependencyDirective {
	if d != nil {
		d.ReconcileTransitionFunc = reconcile
	}
	return d
}

func (d *DependencyDirective) ExpandChanges(ctx context.Context, changes registry.ChangeSet, snapshot registry.ProvenancedState) (registry.DirectiveResult, error) {
	if d == nil || d.ChangesFunc == nil {
		return registry.DirectiveResult{}, nil
	}
	return d.ChangesFunc(ctx, changes, snapshot)
}

func (d *DependencyDirective) ReconcileResolution(ctx context.Context, snapshot registry.ProvenancedState, resolution *registry.DependencyResolution) (registry.DirectiveResult, error) {
	if d == nil {
		return registry.DirectiveResult{}, nil
	}
	if d.ReconcileFunc != nil {
		return d.ReconcileFunc(ctx, snapshot, resolution)
	}
	if d.ReconcileTransitionFunc != nil {
		return d.ReconcileTransitionFunc(ctx, snapshot, snapshot, resolution)
	}
	return registry.DirectiveResult{}, nil
}

func (d *DependencyDirective) ReconcileResolutionTransition(ctx context.Context, current registry.ProvenancedState, target registry.ProvenancedState, resolution *registry.DependencyResolution) (registry.DirectiveResult, error) {
	if d == nil || d.ReconcileTransitionFunc == nil {
		return registry.DirectiveResult{}, nil
	}
	return d.ReconcileTransitionFunc(ctx, current, target, resolution)
}

// Expand implements registry.Directive.
func (d *DependencyDirective) Expand(ctx context.Context, op registry.Operation, snapshot registry.ProvenancedState) (registry.DirectiveResult, error) {
	if d == nil || d.ExpandFunc == nil {
		return registry.DirectiveResult{}, nil
	}
	if err := ctx.Err(); err != nil {
		return registry.DirectiveResult{}, err
	}

	return d.ExpandFunc(ctx, op, snapshot)
}
