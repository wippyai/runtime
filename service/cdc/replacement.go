// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"errors"
)

// Replace starts a candidate before changing visibility whenever the slot is
// running or the candidate is configured for auto-start. A failed handoff
// never publishes the candidate; any failed candidate cleanup is retained as
// retired work for a later Stop/Delete retry.
func (s *sourceSlot) Replace(ctx context.Context, candidate ManagedSource, oldLease, candidateLease leaseRef) error {
	if isNilSource(candidate) {
		return ErrDriverRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()

	s.mu.Lock()
	old := s.current
	oldState := s.state
	oldRunCancel := s.runCancel
	disposing := s.disposing
	hasRetired := len(s.retired) > 0
	s.mu.Unlock()

	oldKey := exclusiveResourceKey(old)
	candidateKey := exclusiveResourceKey(candidate)
	differentResource := oldKey != candidateKey
	if disposing || hasRetired || oldState == slotStopping {
		cleanupErr := s.cleanupCandidate(ctx, candidate, candidateLease, differentResource, false)
		return errors.Join(ErrSourceBusy, cleanupErr)
	}

	// Re-check under the slot lock after calculating resource identity. No
	// other lifecycle operation can replace current while opMu is held, but a
	// status watcher may still have changed the state.
	s.mu.Lock()
	if s.disposing || len(s.retired) > 0 || s.state == slotStopping {
		s.replacing = false
		s.mu.Unlock()
		cleanupErr := s.cleanupCandidate(ctx, candidate, candidateLease, differentResource, false)
		return errors.Join(ErrSourceBusy, cleanupErr)
	}
	s.replacing = true
	s.mu.Unlock()

	startCandidate := oldState == slotRunning || lifecycleAutoStart(candidate)
	// A source configured with a startup snapshot may have accepted a
	// pre-start subscription while the stable slot was idle. Stop that old
	// generation on replacement so its driver can close the prepared stream;
	// ordinary idle sources retain the historical no-op handoff.
	oldHasStartupSnapshot := oldState == slotIdle && !isNilSource(old) && old.Info().Snapshot
	shouldStopOld := !isNilSource(old) &&
		(oldState != slotStopped && oldState != slotIdle || differentResource || oldKey == "" || oldHasStartupSnapshot)

	var (
		underlying <-chan any
		runCtx     context.Context
		runCancel  context.CancelFunc
	)
	speculative := differentResource && startCandidate
	startCandidateGeneration := func() error {
		runCtx, runCancel = detachedContext(ctx)
		var err error
		underlying, err = startSource(ctx, runCtx, candidate)
		if err != nil {
			runCancel()
			cleanupErr := s.cleanupCandidate(ctx, candidate, candidateLease, differentResource, true)
			return errors.Join(err, cleanupErr)
		}
		return nil
	}
	if speculative {
		// Different resource keys may be prepared in parallel. The candidate
		// remains private until old Stop and Dispose both commit below.
		if err := startCandidateGeneration(); err != nil {
			return s.resetReplaceFailure(oldState, err)
		}
	}

	// The old source is stopped before destructive cleanup. A speculative
	// candidate may already be running, but it is not visible through the slot
	// until this handoff has completed.
	oldStopped := !shouldStopOld
	if shouldStopOld {
		if err := stopGeneration(ctx, old, oldRunCancel); err != nil {
			var cleanupErr error
			if speculative {
				runCancel()
				cleanupErr = s.cleanupCandidate(ctx, candidate, candidateLease, differentResource, true)
			} else {
				cleanupErr = s.cleanupCandidate(ctx, candidate, candidateLease, differentResource, false)
			}
			s.mu.Lock()
			s.state = slotFaulted
			s.replacing = false
			s.closeStatusLocked()
			s.runCtx = nil
			s.runCancel = nil
			s.mu.Unlock()
			return errors.Join(err, cleanupErr)
		}
		oldStopped = true
		s.mu.Lock()
		s.state = slotStopped
		s.runCtx = nil
		s.runCancel = nil
		s.closeStatusLocked()
		s.mu.Unlock()
	}

	if differentResource && !isNilSource(old) {
		if disposable, ok := old.(Disposable); ok {
			if err := disposable.Dispose(ctx); err != nil {
				// Keep the old source current and retain its lease. Stop retries
				// this pending destructive cleanup during shutdown or delete.
				s.recordRetired(old, oldKey, oldLease.token, true)
				s.mu.Lock()
				s.state = slotFaulted
				s.replacing = false
				s.mu.Unlock()
				var cleanupErr error
				if speculative {
					runCancel()
				}
				cleanupErr = s.cleanupCandidate(ctx, candidate, candidateLease, differentResource, speculative)
				return errors.Join(err, cleanupErr)
			}
			if oldKey != "" {
				s.mu.RLock()
				hook := s.retiredHook
				s.mu.RUnlock()
				if hook != nil {
					hook(oldKey, oldLease.token)
				}
			}
		}
	}

	if startCandidate && !speculative {
		if err := startCandidateGeneration(); err != nil {
			_, oldDisposable := old.(Disposable)
			restoreOld := oldState == slotRunning && (!differentResource || !oldDisposable)
			if restoreOld && oldStopped {
				// The old generation still owns the shared resource. Restore it
				// before making the failed update visible to the caller.
				restoreCtx, restoreCancel := detachedContext(ctx)
				oldUpdates, restoreErr := startSource(ctx, restoreCtx, old)
				if restoreErr == nil {
					s.mu.Lock()
					s.state = slotRunning
					s.generation++
					s.runCtx = restoreCtx
					s.runCancel = restoreCancel
					s.replacing = false
					generation := s.generation
					if s.status == nil || s.statusDone {
						s.status = make(chan any, 8)
						s.statusDone = false
					}
					s.mu.Unlock()
					s.watchStatus(old, generation, oldUpdates)
					return err
				}
				restoreCancel()
				return s.finishReplaceFailure(errors.Join(err, restoreErr))
			}
			return s.finishReplaceFailure(err)
		}
	}

	s.mu.Lock()
	if s.disposing || s.state == slotStopping {
		s.replacing = false
		s.state = slotFaulted
		s.closeStatusLocked()
		s.runCtx = nil
		s.runCancel = nil
		s.mu.Unlock()
		if runCancel != nil {
			runCancel()
		}
		cleanupErr := s.cleanupCandidate(ctx, candidate, candidateLease, differentResource, startCandidate)
		return errors.Join(ErrSourceBusy, cleanupErr)
	}
	s.current = candidate
	s.generation++
	if startCandidate {
		if s.status == nil || s.statusDone {
			s.status = make(chan any, 8)
			s.statusDone = false
		}
		s.state = slotRunning
		s.runCtx = runCtx
		s.runCancel = runCancel
	} else if oldState == slotIdle {
		s.state = slotIdle
	} else {
		s.state = slotStopped
	}
	s.replacing = false
	generation := s.generation
	s.mu.Unlock()

	if startCandidate {
		s.watchStatus(candidate, generation, underlying)
	}
	return nil
}

func (s *sourceSlot) finishReplaceFailure(err error) error {
	s.mu.Lock()
	s.state = slotFaulted
	s.closeStatusLocked()
	s.runCtx = nil
	s.runCancel = nil
	s.replacing = false
	s.mu.Unlock()
	return err
}

func (s *sourceSlot) resetReplaceFailure(state slotState, err error) error {
	s.mu.Lock()
	s.state = state
	s.replacing = false
	s.mu.Unlock()
	return err
}
