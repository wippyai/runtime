// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"context"
	"errors"
	"time"
)

// WithdrawLocal closes the shared node admission guard, disarms locally owned
// intents and tombstones local-origin bindings. Remote-origin bindings survive.
// The replica remains available for replication. Cancellation leaves admission
// closed; retry finishes a partial sweep. It does not retire cluster inventory,
// prove peer convergence, or stop application processes.
func (s *Service) WithdrawLocal(ctx context.Context) error {
	if ctx == nil || s.cfg.NameGuard == nil {
		return errors.New("eventual withdrawal requires a context and shared name guard")
	}
	if err := s.cfg.NameGuard.Close(ctx); err != nil {
		return err
	}
	s.ownedMu.Lock()
	defer s.ownedMu.Unlock()
	s.withdrawn = true
	clear(s.owned)
	for i := range s.state.shards {
		if err := ctx.Err(); err != nil {
			return err
		}
		sh := &s.state.shards[i]
		sh.mu.RLock()
		names := make([]string, 0)
		for name, rec := range sh.entries {
			if dot := rec.dots[s.state.localNode]; dot != nil && !dot.Deleted {
				names = append(names, name)
			}
		}
		sh.mu.RUnlock()
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return err
			}
			if e := s.state.Unregister(name, time.Now().UnixMilli()); e != nil {
				s.queue.Push(e)
			}
		}
	}
	s.tel.setEntries(s.state.LiveCount(), s.state.TombstoneCount())
	s.tel.setQueueDepth(s.queue.Depth())
	return nil
}
