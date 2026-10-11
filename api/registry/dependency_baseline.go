// SPDX-License-Identifier: MPL-2.0

package registry

import "context"

type dependencyBaselineContextKey struct{}

type dependencyBaseline struct {
	state   State
	changes []ChangeSet
}

// WithDependencyBaseline carries the immutable deployment state before history
// or module materialization. Directives must treat this borrowed state as
// read-only. It is operation-scoped, not an application or Lua configuration.
// Changes carries dependency transactions in replay order so materialization can
// preserve edits newer than the operation that replaced their owner package.
func WithDependencyBaseline(ctx context.Context, baseline State, changes []ChangeSet) context.Context {
	return context.WithValue(ctx, dependencyBaselineContextKey{}, dependencyBaseline{state: baseline, changes: changes})
}

// DependencyBaselineFromContext distinguishes a known empty deployment from a
// caller that has not supplied its deployment state.
func DependencyBaselineFromContext(ctx context.Context) (State, bool) {
	if ctx == nil {
		return nil, false
	}
	baseline, ok := ctx.Value(dependencyBaselineContextKey{}).(dependencyBaseline)
	return baseline.state, ok
}

// DependencyChangesFromContext returns the ordered dependency transactions captured
// during the registry's existing replay. Directives must not modify it.
func DependencyChangesFromContext(ctx context.Context) []ChangeSet {
	if ctx == nil {
		return nil
	}
	baseline, _ := ctx.Value(dependencyBaselineContextKey{}).(dependencyBaseline)
	return baseline.changes
}
