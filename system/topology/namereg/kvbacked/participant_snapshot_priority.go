// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"sync"
)

// Priority is restricted to the cancellable authority read, not snapshot
// installation or vote writes. Existing serialization still joins those phases.
type participantSnapshotPriority struct {
	mu      sync.Mutex
	cleanup int
	retry   bool
	cancel  context.CancelFunc
}

// Caller holds participantRefresh until finish returns.
func (p *participantSnapshotPriority) background(ctx context.Context) (context.Context, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cleanup != 0 {
		p.retry = true
		return nil, nil, errParticipantAuthorityBusy
	}
	readCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	return readCtx, func() {
		p.mu.Lock()
		p.cancel = nil
		p.mu.Unlock()
		cancel()
	}, nil
}

// Mark cleanup before acquiring the shared gate: a refresh acquiring the gate
// concurrently cannot install a new read after missing this cancellation.
func (p *participantSnapshotPriority) admitCleanup() func() bool {
	p.mu.Lock()
	p.cleanup++
	if p.cancel != nil {
		p.retry = true
		p.cancel()
	}
	p.mu.Unlock()
	return func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.cleanup--
		if p.cleanup == 0 && p.retry {
			p.retry = false
			return true
		}
		return false
	}
}

func (s *Service) prioritizeParticipantCleanup() func() {
	finish := s.strong.snapshotPriority.admitCleanup()
	return func() {
		if finish() {
			s.requestParticipantRefresh()
		}
	}
}
