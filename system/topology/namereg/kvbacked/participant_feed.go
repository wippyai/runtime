// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"fmt"
	"time"
)

// startParticipantFeed owns client refresh without subscribing to its empty
// local replica. This internal integration requires an authority whose Strong
// reservations use the committed inventory. Boot must cut over both together.
// interval is the configured backstop for lost hints, not a claim-expiry lease.
func (s *Service) startParticipantFeed(ctx context.Context, inventory *participantInventory, source participantSnapshotSource, limits participantSnapshotLimits, interval time.Duration) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.strong == nil || inventory == nil || source == nil || interval <= 0 || limits.MaxEntries <= 0 || limits.MaxValueBytes <= 0 {
		return fmt.Errorf("invalid participant feed configuration")
	}
	if s.nonMember == nil || !s.nonMember() {
		return fmt.Errorf("participant feed requires a forwarding client")
	}
	ctx, cancel := context.WithCancel(ctx)
	run := &reconcilerLifecycle{ctx: ctx, cancel: cancel, done: make(chan struct{}), refresh: make(chan struct{}, 1)}
	if !s.reconciler.CompareAndSwap(nil, run) {
		cancel()
		return fmt.Errorf("registry reconciler already started; use a new service after shutdown")
	}
	// Only the winner of lifetime admission may reopen timer ownership. A
	// failed prior startup has joined its timers before releasing this slot.
	s.strong.mu.Lock()
	s.strong.timersSealed = false
	s.strong.mu.Unlock()
	s.ready.Store(false)
	dissemStarted := false
	finish := func() {
		s.ready.Store(false)
		run.closeAdmission()
		if dissemStarted {
			s.dissem.Stop()
		}
		_ = s.strong.stopTimers(context.Background())
		run.calls.Wait()
		run.workers.Wait()
		close(run.done)
	}
	defer func() {
		if err != nil {
			finish()
			s.reconciler.CompareAndSwap(run, nil)
		}
	}()
	if _, err = s.bootstrapParticipant(ctx, inventory, source, limits); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if s.dissem != nil {
		dissemStarted = true
		run.workers.Go(s.dissem.RunGC)
	}
	run.bootstrapped.Store(true)
	s.ready.Store(true)
	go func() {
		defer finish()
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-run.refresh:
			case <-timer.C:
			}
			if ctx.Err() != nil {
				return
			}
			_, refreshErr := s.refreshParticipant(ctx, inventory, source, limits)
			if ctx.Err() != nil {
				return
			}
			// Failed refresh has already closed readiness and retained conservative
			// exclusions. A subsequent complete refresh may restore admission.
			if refreshErr == nil {
				s.ready.Store(true)
			}
			// Every hint performs the same complete refresh as the timer; count
			// the backstop from completion to avoid a redundant queued scan.
			timer.Reset(interval)
		}
	}()
	return nil
}

// requestParticipantRefresh coalesces arbitrary update hints into one pending
// refresh. No per-event goroutine or event-bus queue is created or retained.
func (s *Service) requestParticipantRefresh() {
	run := s.reconciler.Load()
	if run == nil || run.refresh == nil || run.ctx.Err() != nil {
		return
	}
	select {
	case run.refresh <- struct{}{}:
	default:
	}
}
