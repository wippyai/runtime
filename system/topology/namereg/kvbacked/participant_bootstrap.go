// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/pid"
)

// bootstrapParticipant commits participation before capturing and installing
// exclusions. The caller owns startup serialization and keeps admission closed
// until its refresh lifecycle is installed. Failure does not retire enrollment.
// This does not start a local KV watcher: clients have no authoritative local
// replica, so their subsequent reconciliation must use the authority feed.
func (s *Service) bootstrapParticipant(ctx context.Context, inventory *participantInventory, source participantSnapshotSource, limits participantSnapshotLimits) (uint64, error) {
	if s.strong == nil || inventory == nil {
		return 0, fmt.Errorf("participant bootstrap requires Strong configuration and inventory")
	}
	if s.ready.Load() {
		return 0, fmt.Errorf("participant bootstrap requires closed naming admission")
	}
	if remote, ok := source.(interface{ participantIdentity() (pid.NodeID, string) }); ok {
		// The mesh authority enrolls the authenticated peer before capture.
		// Never enroll separately through a client's forwarded KV surface.
		node, incarnation := remote.participantIdentity()
		if node != s.selfNode || incarnation != s.strong.incarnation {
			return 0, fmt.Errorf("participant source identity does not match service")
		}
	} else if err := inventory.enroll(ctx, s.selfNode, s.strong.incarnation); err != nil {
		return 0, err
	}
	return s.syncParticipant(ctx, inventory, source, limits, true)
}

// refreshParticipant serializes complete authority captures and installation.
// Only obligations observed before the capture may be retired by its absence
// evidence; newer local generations survive. New pending claims use normal
// conflict attestation, never bootstrap withdrawal. It does not open admission.
func (s *Service) refreshParticipant(ctx context.Context, inventory *participantInventory, source participantSnapshotSource, limits participantSnapshotLimits) (revision uint64, err error) {
	return s.syncParticipant(ctx, inventory, source, limits, false)
}

func (s *Service) syncParticipant(ctx context.Context, inventory *participantInventory, source participantSnapshotSource, limits participantSnapshotLimits, bootstrap bool) (revision uint64, err error) {
	if s.strong == nil || inventory == nil {
		return 0, fmt.Errorf("participant refresh requires Strong configuration and inventory")
	}
	defer func() {
		if err != nil {
			s.ready.Store(false)
		}
	}()
	release, err := s.strong.participantRefresh.LockContext(ctx, "")
	if err != nil {
		return 0, err
	}
	defer release()
	s.strong.mu.Lock()
	observed := make(map[string]terminalObservation, len(s.strong.exclusions))
	for name, exclusion := range s.strong.exclusions {
		observed[name] = terminalObservation{exclusion: exclusion, held: true, timer: s.strong.timers[name], waiters: s.strong.boundWaitersLocked(name)}
	}
	s.strong.mu.Unlock()
	readCtx, finishRead, err := s.strong.snapshotPriority.background(ctx)
	if err != nil {
		return 0, err
	}
	snapshot, err := inventory.captureSnapshot(readCtx, source, s.selfNode, s.strong.incarnation, limits)
	finishRead()
	if err != nil {
		return 0, err
	}
	if snapshot.Revision < s.strong.participantRevision {
		return 0, fmt.Errorf("participant snapshot revision regressed")
	}
	present := make(map[string]struct{})
	for _, exclusion := range snapshot.exclusions {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		process, err := pid.ParsePID(exclusion.owner)
		if err != nil {
			return 0, err
		}
		present[exclusion.name] = struct{}{}
		if exclusion.pending != nil && !bootstrap {
			if err := s.strong.attestHeaderContext(ctx, exclusion.name, exclusion.epoch, process, *exclusion.pending); err != nil {
				return 0, err
			}
			continue
		}
		if exclusion.pending == nil && !bootstrap {
			// A forwarding client has no replicated watch to complete its Strong
			// waiter. The fresh authority snapshot is its terminal observation.
			if err := s.strong.onActiveContext(ctx, exclusion.name, exclusion.epoch, process, exclusion.attemptID); err != nil {
				return 0, err
			}
			continue
		}
		state := exclusionActive
		if exclusion.pending != nil {
			state = exclusionPending
		}
		// Bootstrap installs exclusions without ACKs or leader-side transitions,
		// including reservations predating this incarnation's enrollment.
		if _, err := s.strong.learnExclusionContext(ctx, exclusion.name, process, exclusion.epoch, state); err != nil {
			return 0, err
		}
	}

	if err := ctx.Err(); err != nil {
		return 0, err
	}
	for name, prior := range observed {
		if _, exists := present[name]; exists {
			continue
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		s.strong.onTerminal(name, prior, snapshot.Revision)
	}
	s.strong.participantRevision = snapshot.Revision
	return snapshot.Revision, nil
}
