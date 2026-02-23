// SPDX-License-Identifier: MPL-2.0

package expansion

import (
	"context"

	"github.com/wippyai/runtime/api/registry"
)

// DependencyDirectiveFunc provides expansion for dependency entries.
// Implementations should honor the provided context.
type DependencyDirectiveFunc func(ctx context.Context, op registry.Operation, snapshot registry.State) (registry.DirectiveResult, error)

// DependencyDirective expands dependency operations using a direct handler.
type DependencyDirective struct {
	ExpandFunc DependencyDirectiveFunc
}

// NewDependencyDirective constructs a dependency directive backed by the given handler.
func NewDependencyDirective(expand DependencyDirectiveFunc) *DependencyDirective {
	return &DependencyDirective{ExpandFunc: expand}
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
