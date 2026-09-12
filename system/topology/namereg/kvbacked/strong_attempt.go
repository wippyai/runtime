// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

// observeCreatedActive resolves creation racing promotion without adopting a
// later claim, including one made by the same PID. It uses the authoritative
// read path: a follower's local cache may predate the creation transaction.
// Absence is uncertainty, not proof that the accepted reservation failed.
// Read retained attempt evidence first; the active record is a fallback only
// after evidence reclamation, and still must match the exact attempt.
func (st *strongState) observeCreatedActive(ctx context.Context, name string, owner pid.PID, attemptID string) (globalapi.RegisterOutcome, error) {
	outcome, outcomeErr := st.attemptOutcome(ctx, name, owner, attemptID)
	if !errors.Is(outcomeErr, kvapi.ErrKeyNotFound) {
		return outcome, outcomeErr
	}
	entry, err := st.svc.get(activeKey(name))
	if err != nil && !errors.Is(err, kvapi.ErrKeyNotFound) {
		return globalapi.RegisterOutcome{}, err
	}
	if err == nil {
		active, err := decodeActive(entry.Value)
		if err != nil {
			return globalapi.RegisterOutcome{}, err
		}
		if attemptID != "" && active.AttemptID == attemptID && active.Strong && active.Name == name && active.PID == owner.String() {
			return globalapi.RegisterOutcome{PID: owner, Epoch: entry.Epoch, State: globalapi.RegisterStateActive}, nil
		}
	}
	return globalapi.RegisterOutcome{}, fmt.Errorf("strong registration %q: attempt outcome unavailable: %w", name, globalapi.ErrNotAvailable)
}

// A preinstalled waiter can retain exact success even if the active record
// has already been removed by the time the creator resumes its read.
func (st *strongState) observeCreatedWaiter(ctx context.Context, name string, owner pid.PID, waiter *strongWaiter) (globalapi.RegisterOutcome, error) {
	select {
	case out := <-waiter.ch:
		return st.finalizeAttempt(ctx, name, owner, waiter.attemptID, out)
	default:
	}
	out, err := st.observeCreatedActive(ctx, name, owner, waiter.attemptID)
	if err != nil {
		select {
		case delivered := <-waiter.ch:
			return st.finalizeAttempt(ctx, name, owner, waiter.attemptID, delivered)
		default:
		}
	}
	return out, err
}

// Caller holds mu. Unbound waiters have not learned a committed pending epoch,
// so a name's absence cannot yet be attributed to their attempt. Once bound,
// epoch never changes and captured terminal observations can read it safely.
func (st *strongState) boundWaitersLocked(name string) []*strongWaiter {
	var bound []*strongWaiter
	for _, waiter := range st.waiters[name] {
		if !waiter.awaitingEpoch {
			bound = append(bound, waiter)
		}
	}
	return bound
}
