// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
)

// sealParticipantMutations permanently refuses direct mutations and joins those
// already admitted, while retaining reconciliation/attestation. This is only one
// retirement prerequisite: the shared weaker-scope NameGuard must also close,
// and bindings must be withdrawn before committing inventory retirement.
// Cancellation bounds joining, not the seal; retry joins the same lifetime.
func (s *Service) sealParticipantMutations(ctx context.Context) error {
	if ctx == nil {
		return errors.New("participant seal requires a context")
	}
	run := s.reconciler.Load()
	if run == nil || s.strong == nil || s.strong.participants == nil {
		return errors.New("participant seal requires a running enrolled lifecycle")
	}
	run.admission.Lock()
	if run.mutationsDrained == nil {
		run.mutationsSealed.Store(true)
		run.mutationsDrained = make(chan struct{})
		if run.mutationCount == 0 {
			close(run.mutationsDrained)
		}
	}
	done := run.mutationsDrained
	run.admission.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
