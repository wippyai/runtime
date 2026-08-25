// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"errors"
	"sync"
)

// RollbackOutcome collects what a runner's compensation actually did, so the
// registry can reconstruct the provenance of a partially compensated state
// instead of guessing from the map it published before the transition.
type RollbackOutcome struct {
	surviving []Operation
	errs      []error
	mu        sync.Mutex
}

// Record notes one accepted operation's compensation attempt. A failed
// compensation leaves the operation's effect in the state.
func (o *RollbackOutcome) Record(op Operation, compensated bool, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !compensated {
		o.surviving = append(o.surviving, op)
	}
	if err != nil {
		o.errs = append(o.errs, err)
	}
}

// Surviving returns the accepted operations whose effects remain in the state.
func (o *RollbackOutcome) Surviving() []Operation {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]Operation(nil), o.surviving...)
}

// Err joins the compensation failures.
func (o *RollbackOutcome) Err() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return errors.Join(o.errs...)
}

type rollbackOutcomeContextKey struct{}

// WithRollbackOutcome attaches a compensation collector for the runner to fill.
func WithRollbackOutcome(ctx context.Context, o *RollbackOutcome) context.Context {
	return context.WithValue(ctx, rollbackOutcomeContextKey{}, o)
}

// RollbackOutcomeFromContext returns the collector, if the caller supplied one.
func RollbackOutcomeFromContext(ctx context.Context) *RollbackOutcome {
	if ctx == nil {
		return nil
	}
	o, _ := ctx.Value(rollbackOutcomeContextKey{}).(*RollbackOutcome)
	return o
}
