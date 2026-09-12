// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"fmt"
)

// startParticipantMember retains the local replica watcher and leader sweep,
// while requiring committed enrollment and authority exclusions before naming
// admission. source may be local leader state or a peer-bound mesh source.
// The caller configures inventory reservation creation before starting any node.
func (s *Service) startParticipantMember(ctx context.Context, inventory *participantInventory, source participantSnapshotSource, limits participantSnapshotLimits) error {
	if s.strong == nil || inventory == nil || source == nil || limits.MaxEntries <= 0 || limits.MaxValueBytes <= 0 {
		return fmt.Errorf("invalid member participation configuration")
	}
	if s.strong.participants != inventory {
		return fmt.Errorf("member reservation inventory must match enrollment inventory")
	}
	if s.nonMember != nil && s.nonMember() {
		return fmt.Errorf("member participation requires a local replica")
	}
	return s.startReconciler(ctx, func(ctx context.Context) error {
		_, err := s.bootstrapParticipant(ctx, inventory, source, limits)
		return err
	}, s.strong.recovery)
}
