// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// participantCachedSource borrows an immutable decoded response for validation.
// It is not exposed to application visitors; those receive copied value bytes.
type participantCachedSource struct{ snapshot *participantWireResponse }

func (s participantCachedSource) ScanAtIndex(_ string, visit func(kvapi.Entry) bool) (uint64, error) {
	for _, entry := range s.snapshot.Entries {
		if !visit(entry) {
			break
		}
	}
	return s.snapshot.Revision, nil
}

// Cache only complete, bounded naming snapshots for this exact incarnation.
// Participant cardinality is distinct from KV-entry cardinality. Standalone
// internal clients default to the conservative wire-byte bound; endpoint
// assembly supplies its actual configured participant limit.
// A consumer's stricter participant/byte limits are still checked on each scan;
// failure to qualify for caching never relaxes those checks or implies absence.
func (c *participantClient) cacheableSnapshot(ctx context.Context, snapshot *participantWireResponse) bool {
	inventory := &participantInventory{maxParticipants: c.maxParticipants}
	_, err := inventory.captureSnapshot(ctx, participantCachedSource{snapshot}, c.self, c.incarnation, participantSnapshotLimits{MaxEntries: c.maxEntries, MaxValueBytes: c.maxBytes})
	return err == nil
}
