// SPDX-License-Identifier: MPL-2.0

package expansion

import (
	"context"

	"github.com/wippyai/runtime/api/registry"
)

// DependencyDirectiveFunc provides expansion for dependency entries.
// Implementations should honor the provided context.
type DependencyDirectiveFunc func(ctx context.Context, op registry.Operation, snapshot registry.State) (registry.DirectiveResult, error)
type DependencyChangesDirectiveFunc func(ctx context.Context, changes registry.ChangeSet, snapshot registry.State) (registry.DirectiveResult, error)
type ResolutionDirectiveFunc func(ctx context.Context, snapshot registry.State, resolution *registry.DependencyResolution) (registry.DirectiveResult, error)

// DependencyDirective expands dependency operations using a direct handler.
type DependencyDirective struct {
	ExpandFunc    DependencyDirectiveFunc
	ChangesFunc   DependencyChangesDirectiveFunc
	ReconcileFunc ResolutionDirectiveFunc
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

func (d *DependencyDirective) ExpandChanges(ctx context.Context, changes registry.ChangeSet, snapshot registry.State) (registry.DirectiveResult, error) {
	if d == nil || d.ChangesFunc == nil {
		return registry.DirectiveResult{}, nil
	}
	return d.ChangesFunc(ctx, changes, snapshot)
}

func (d *DependencyDirective) ReconcileResolution(ctx context.Context, snapshot registry.State, resolution *registry.DependencyResolution) (registry.DirectiveResult, error) {
	if d == nil || d.ReconcileFunc == nil {
		return registry.DirectiveResult{}, nil
	}
	return d.ReconcileFunc(ctx, snapshot, resolution)
}

// Expand implements registry.Directive.
func (d *DependencyDirective) Expand(ctx context.Context, op registry.Operation, snapshot registry.State) (registry.DirectiveResult, error) {
	if d == nil || d.ExpandFunc == nil {
		return registry.DirectiveResult{}, nil
	}
	if err := ctx.Err(); err != nil {
		return registry.DirectiveResult{}, err
	}

	return d.ExpandFunc(ctx, op, snapshot)
}
