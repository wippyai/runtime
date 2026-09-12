// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"strings"

	"github.com/wippyai/runtime/api/pid"
)

// reapSnapshot enumerates active owners in the existing bounded naming snapshot.
// Forwarding clients have no reverse-index replica. The snapshot is fresh at its
// authority revision; each delete still checks current owner/version, so later
// replacement cannot be erased. No partial snapshot is ever used as enumeration.
func (s *Service) reapSnapshot(ctx context.Context, base string, preserveParticipants bool) error {
	snapshot, err := s.cleanupSnapshot(ctx)
	if err != nil {
		return err
	}
	var cleanupErr error
	for key, entry := range snapshot.Entries {
		if err := ctx.Err(); err != nil {
			return errors.Join(cleanupErr, err)
		}
		if !strings.HasPrefix(key, activePrefix) {
			continue
		}
		active, err := decodeActive(entry.Value)
		if err != nil {
			return errors.Join(cleanupErr, err)
		}
		owner, err := pid.ParsePID(active.PID)
		if err != nil {
			return errors.Join(cleanupErr, err)
		}
		if !strings.HasPrefix(pidIndexKey(owner, active.Name), base) && !strings.HasPrefix(nodeIndexKey(owner, active.Name), base) {
			continue
		}
		cleanupErr = errors.Join(cleanupErr, s.deleteBindingWithPolicyContext(ctx, owner, active.Name, preserveParticipants))
	}
	return cleanupErr
}
