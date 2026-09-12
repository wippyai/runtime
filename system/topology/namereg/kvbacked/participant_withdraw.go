// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"fmt"
)

// retireParticipant composes the voluntary local prerequisites with the exact
// inventory transition. withdraw must cover every weaker registry using this
// node's shared guard; a successful callback attests local withdrawal only.
// ParticipantEndpoint.Retire exposes this to the trusted native lifecycle owner;
// boot uses it before endpoint shutdown; it is not a separate mesh operation.
// The lifecycle owner must subsequently join endpoint/transport shutdown before
// allowing a fresh incarnation. It must not interpret success as process exit
// or proof that every remote replica has consumed the withdrawal tombstones.
func (s *Service) retireParticipant(ctx context.Context, withdraw func(context.Context) error) error {
	if ctx == nil || withdraw == nil || s.strong == nil || s.strong.nameGuard == nil || s.strong.participants == nil {
		return errors.New("participant retirement requires configured guard, inventory and withdrawal")
	}
	// Only one caller runs the withdrawal/commit sequence at a time. Canceling
	// admission or joining never reverses an earlier seal or repeats an uncertain
	// transaction automatically; a caller-directed retry resolves committed state.
	release, err := s.strong.retirement.LockContext(ctx, "")
	if err != nil {
		return err
	}
	defer release()
	if err := s.sealParticipantMutations(ctx); err != nil {
		return fmt.Errorf("seal mutations: %w", err)
	}
	if err := s.strong.nameGuard.Close(ctx); err != nil {
		return fmt.Errorf("close name guard: %w", err)
	}
	if err := withdraw(ctx); err != nil {
		return fmt.Errorf("withdraw weaker names: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err = s.strong.participants.retire(ctx, s.selfNode, s.strong.incarnation)
	if err != nil {
		return fmt.Errorf("commit retirement: %w", err)
	}
	return nil
}
