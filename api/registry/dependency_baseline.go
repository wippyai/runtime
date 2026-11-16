// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"

	"github.com/wippyai/runtime/api/payload"
)

type dependencyBaselineContextKey struct{}

type dependencyBaseline struct {
	replay  *ReplayState
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

// WithDependencyReplayState carries the compact replay projection alongside
// the deployment baseline. The projection is immutable once attached to a
// directive context.
func WithDependencyReplayState(ctx context.Context, baseline State, state *ReplayState) context.Context {
	changes := DependencyChangesFromContext(ctx)
	return context.WithValue(ctx, dependencyBaselineContextKey{}, dependencyBaseline{state: baseline, changes: changes, replay: state})
}

// DependencyReplayStateFromContext returns the compact replay projection when
// the registry supplied one for the current restore or version transition.
func DependencyReplayStateFromContext(ctx context.Context) (*ReplayState, bool) {
	if ctx == nil {
		return nil, false
	}
	baseline, ok := ctx.Value(dependencyBaselineContextKey{}).(dependencyBaseline)
	return baseline.replay, ok && baseline.replay != nil
}

// DependencyReplayState returns the compact replay projection for ctx. Legacy
// callers that carry ordered changesets are reduced through the same state
// reducer used by checkpoint-backed restores.
func DependencyReplayState(ctx context.Context, transcoder payload.Transcoder) (*ReplayState, error) {
	if state, ok := DependencyReplayStateFromContext(ctx); ok {
		return state, nil
	}
	state := NewReplayState()
	for _, changes := range DependencyChangesFromContext(ctx) {
		if err := state.ApplyDependencyChanges(changes, transcoder); err != nil {
			return nil, err
		}
	}
	return state, nil
}
