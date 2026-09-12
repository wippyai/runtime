// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"fmt"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"time"
)

type participantRecovery struct {
	interval time.Duration
	refresh  func(context.Context) error
}

// recoverParticipantWatch runs within the reconciler owner. It bounds retry
// frequency, retains exclusions while unavailable, and subscribes before the
// authority refresh so writes during capture are not lost. No write payload or
// prior transaction result is replayed; reconciliation observes current state.
func (s *Service) recoverParticipantWatch(ctx context.Context, policy *participantRecovery) (kvapi.Watcher, error) {
	for {
		timer := time.NewTimer(policy.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		run := s.reconciler.Load()
		run.admission.Lock()
		generation := run.recoveryGeneration
		select {
		case <-run.recover:
		default:
		}
		run.admission.Unlock()
		watcher, err := s.engine.Watch(ctx, registryPrefix)
		if err != nil {
			continue
		}
		if err = policy.refresh(ctx); err == nil {
			err = s.seed()
		}
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			_ = watcher.Close()
			continue
		}
		// Drain already queued updates before readiness. A watcher that overflowed
		// during capture closes; reacquire it rather than opening on a known gap.
	drain:
		for {
			select {
			case <-ctx.Done():
				err = ctx.Err()
				break drain
			case event, ok := <-watcher.Events():
				if !ok {
					err = fmt.Errorf("registry update stream closed during recovery")
					break drain
				}
				if err = s.handleWatchEvent(event); err != nil {
					break drain
				}
			default:
				break drain
			}
		}
		if err != nil {
			_ = watcher.Close()
			continue
		}
		run.admission.Lock()
		valid := !run.stopping && ctx.Err() == nil && generation == run.recoveryGeneration
		if valid {
			s.ready.Store(true)
		}
		run.admission.Unlock()
		if !valid {
			_ = watcher.Close()
			continue
		}
		return watcher, nil
	}
}

// invalidateParticipant coalesces independent timer/sweep errors into the
// existing owner. The generation prevents an older refresh reopening readiness
// over a newer failure, including one arriving as that refresh completes.
func (s *Service) invalidateParticipant() {
	run := s.reconciler.Load()
	if run == nil {
		s.ready.Store(false)
		return
	}
	run.admission.Lock()
	s.ready.Store(false)
	run.recoveryGeneration++
	if !run.stopping && run.ctx.Err() == nil {
		select {
		case run.recover <- struct{}{}:
		default:
		}
	}
	run.admission.Unlock()
}
