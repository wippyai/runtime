// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

type enrollmentDuringSnapshot struct {
	participantSnapshotSource
	enroll func()
}

func (s enrollmentDuringSnapshot) ScanAtIndex(prefix string, fn func(kvapi.Entry) bool) (uint64, error) {
	first := true
	return s.participantSnapshotSource.ScanAtIndex(prefix, func(entry kvapi.Entry) bool {
		if first {
			first = false
			s.enroll()
		}
		return fn(entry)
	})
}

func TestParticipantSnapshotCannotBorrowLaterEnrollment(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	require.NoError(t, inventory.enroll(ctx, "node-1", "one"))
	source := enrollmentDuringSnapshot{participantSnapshotSource: engine, enroll: func() {
		require.NoError(t, inventory.enroll(ctx, "node-2", "two"))
	}}
	limits := participantSnapshotLimits{MaxEntries: 10, MaxValueBytes: 4096}
	snapshot, err := inventory.captureSnapshot(ctx, source, "node-2", "two", limits)
	require.ErrorIs(t, err, globalapi.ErrNotAvailable)
	require.Nil(t, snapshot, "enrollment must be present in the captured view itself")
	snapshot, err = inventory.captureSnapshot(ctx, engine, "node-2", "two", limits)
	require.NoError(t, err)
	require.NotZero(t, snapshot.Revision)
}

func TestParticipantSnapshotCompleteBoundedAndIndependent(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	require.NoError(t, inventory.enroll(ctx, "node-1", "one"))
	owner := mkPID("node-1", "owner")
	ops, err := inventory.reservationOps(ctx, pendingHeader{Name: "pending", PID: owner.String()})
	require.NoError(t, err)
	committed, err := engine.Txn(ops)
	require.NoError(t, err)
	require.True(t, committed)
	active, err := encode(activeValue{Name: "active", PID: owner.String()})
	require.NoError(t, err)
	_, err = engine.Set(activeKey("active"), active)
	require.NoError(t, err)
	limits := participantSnapshotLimits{MaxEntries: 3, MaxValueBytes: 4096}
	snapshot, err := inventory.captureSnapshot(ctx, engine, "node-1", "one", limits)
	require.NoError(t, err)
	require.Len(t, snapshot.Entries, 3)
	bytes := 0
	for key, entry := range snapshot.Entries {
		bytes += len(key) + len(entry.Value)
	}
	limits.MaxValueBytes = bytes
	_, err = inventory.captureSnapshot(ctx, engine, "node-1", "one", limits)
	require.NoError(t, err)
	limits.MaxValueBytes--
	failed, err := inventory.captureSnapshot(ctx, engine, "node-1", "one", limits)
	require.Error(t, err)
	require.Nil(t, failed)
	limits.MaxValueBytes = 4096
	limits.MaxEntries = 2
	failed, err = inventory.captureSnapshot(ctx, engine, "node-1", "one", limits)
	require.Error(t, err)
	require.Nil(t, failed)
	snapshot.Entries[activeKey("active")].Value[0] ^= 0xff
	unchanged, err := engine.Get(activeKey("active"))
	require.NoError(t, err)
	require.Equal(t, active, unchanged.Value, "returned snapshot must not mutate authority")
}

type failedParticipantSnapshotSource struct{ err error }

func (s failedParticipantSnapshotSource) ScanAtIndex(string, func(kvapi.Entry) bool) (uint64, error) {
	return 0, s.err
}

func TestParticipantSnapshotFailsClosed(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	limits := participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}
	require.NoError(t, inventory.enroll(ctx, "node-1", "one"))
	snapshot, err := inventory.captureSnapshot(ctx, engine, "node-1", "replacement", limits)
	require.ErrorIs(t, err, ErrParticipantIncarnationConflict)
	require.Nil(t, snapshot)
	failure := errors.New("authority unavailable")
	snapshot, err = inventory.captureSnapshot(ctx, failedParticipantSnapshotSource{failure}, "node-1", "one", limits)
	require.ErrorIs(t, err, failure)
	require.Nil(t, snapshot)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	snapshot, err = inventory.captureSnapshot(canceled, engine, "node-1", "one", limits)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, snapshot)
	_, err = engine.Set(pendingKey("bad"), []byte{0xff})
	require.NoError(t, err)
	snapshot, err = inventory.captureSnapshot(ctx, engine, "node-1", "one", limits)
	require.Error(t, err)
	require.Nil(t, snapshot)
}

// alteredParticipantSnapshot models a malformed remote snapshot adapter while
// retaining real, encoded records from an immutable authority snapshot.
type alteredParticipantSnapshot struct {
	participantSnapshotSource
	alter        func(kvapi.Entry) kvapi.Entry
	zeroRevision bool
}

func (s alteredParticipantSnapshot) ScanAtIndex(prefix string, fn func(kvapi.Entry) bool) (uint64, error) {
	revision, err := s.participantSnapshotSource.ScanAtIndex(prefix, func(entry kvapi.Entry) bool {
		if s.alter != nil {
			entry = s.alter(entry)
		}
		return fn(entry)
	})
	if s.zeroRevision {
		revision = 0
	}
	return revision, err
}

func TestParticipantSnapshotRejectsUnversionedAuthority(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	require.NoError(t, inventory.enroll(ctx, "node-1", "one"))
	owner := mkPID("node-1", "owner")
	active, err := encode(activeValue{Name: "active", PID: owner.String()})
	require.NoError(t, err)
	_, err = engine.Set(activeKey("active"), active)
	require.NoError(t, err)
	ops, err := inventory.reservationOps(ctx, pendingHeader{Name: "pending", PID: owner.String()})
	require.NoError(t, err)
	committed, err := engine.Txn(ops)
	require.NoError(t, err)
	require.True(t, committed)
	limits := participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}
	for _, key := range []string{participantsKey, activeKey("active"), pendingKey("pending")} {
		t.Run(key, func(t *testing.T) {
			source := alteredParticipantSnapshot{participantSnapshotSource: engine, alter: func(entry kvapi.Entry) kvapi.Entry {
				if entry.Key == key {
					entry.Version = 0
				}
				return entry
			}}
			snapshot, err := inventory.captureSnapshot(ctx, source, "node-1", "one", limits)
			require.ErrorContains(t, err, "zero version")
			require.Nil(t, snapshot)
		})
	}
	snapshot, err := inventory.captureSnapshot(ctx, alteredParticipantSnapshot{participantSnapshotSource: engine, zeroRevision: true}, "node-1", "one", limits)
	require.ErrorContains(t, err, "zero authority revision")
	require.Nil(t, snapshot)
}

type futureEpochParticipantSource struct{ participantSnapshotSource }

func (s futureEpochParticipantSource) ScanAtIndex(prefix string, visit func(kvapi.Entry) bool) (uint64, error) {
	return s.participantSnapshotSource.ScanAtIndex(prefix, func(entry kvapi.Entry) bool {
		if entry.Key != participantsKey {
			entry.Epoch = ^uint64(0)
		}
		return visit(entry)
	})
}
func TestParticipantSnapshotRejectsEpochBeyondAuthorityRevision(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	require.NoError(t, inventory.enroll(ctx, "member", "one"))
	owner := mkPID("owner", "process")
	body, err := encode(activeValue{Name: "name", PID: owner.String(), Strong: true})
	require.NoError(t, err)
	_, err = engine.Set(activeKey("name"), body)
	require.NoError(t, err)
	source := futureEpochParticipantSource{engine}
	service := NewService(engine, "member", nil, nil)
	service.ConfigureStrong(StrongDeps{Incarnation: "one", IsLeader: func() bool { return false }})
	_, err = service.bootstrapParticipant(ctx, inventory, source, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096})
	require.Error(t, err, "future epoch would prevent later authoritative absence from releasing the exclusion")
	require.False(t, service.NameReady())
	_, held := service.IsStrongReserved("name")
	require.False(t, held, "invalid snapshot must be rejected before installing exclusions")
}

func TestParticipantSnapshotRejectsRetiredAndReplacedIncarnation(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 2)
	ctx := context.Background()
	limits := participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}
	require.NoError(t, inventory.enroll(ctx, "node", "old"))
	require.NoError(t, inventory.retire(ctx, "node", "old"))
	snapshot, err := inventory.captureSnapshot(ctx, engine, "node", "old", limits)
	require.ErrorIs(t, err, globalapi.ErrNotAvailable)
	require.Nil(t, snapshot)
	require.NoError(t, inventory.replace(ctx, "node", "old", "new"))
	snapshot, err = inventory.captureSnapshot(ctx, engine, "node", "old", limits)
	require.ErrorIs(t, err, ErrParticipantIncarnationConflict)
	require.Nil(t, snapshot)
	snapshot, err = inventory.captureSnapshot(ctx, engine, "node", "new", limits)
	require.NoError(t, err)
	require.NotNil(t, snapshot)
}
