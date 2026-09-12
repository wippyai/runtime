// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

type countedConditionalSource struct {
	participantSnapshotSource
	conditional kvapi.ConditionalSnapshotReader
	visits      int
}

func (s *countedConditionalSource) ScanAtIndexSince(prefix string, known uint64, visit func(kvapi.Entry) bool) (uint64, bool, error) {
	return s.conditional.ScanAtIndexSince(prefix, known, func(entry kvapi.Entry) bool { s.visits++; return visit(entry) })
}

func TestParticipantConditionalAuthorityLifetimeAndRevision(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	source := &countedConditionalSource{participantSnapshotSource: engine, conditional: engine}
	ctx := context.Background()
	authority, err := newParticipantAuthority(ctx, "authority", inventory, source, participantSnapshotLimits{8, 8192}, 1)
	require.NoError(t, err)
	defer authority.stop(ctx)
	request := participantRequestPackage(t, 1, "one")
	defer relay.ReleasePackage(request)
	full, err := authority.snapshotForPackage(ctx, request, "one")
	require.NoError(t, err)
	require.NotEmpty(t, full.Lifetime)
	require.False(t, full.Unchanged)
	require.NotEmpty(t, full.Entries)
	same, err := authority.snapshotForPackageSince(ctx, request, "one", full.Lifetime, full.Revision)
	require.NoError(t, err)
	require.True(t, same.Unchanged)
	require.Equal(t, full.Revision, same.Revision)
	require.Empty(t, same.Entries)
	require.Zero(t, source.visits, "unchanged capture copied/validated entries")
	owner := mkPID("client", "p")
	value, err := encode(activeValue{Name: "changed", PID: owner.String()})
	require.NoError(t, err)
	_, err = engine.Set(activeKey("changed"), value)
	require.NoError(t, err)
	changed, err := authority.snapshotForPackageSince(ctx, request, "one", full.Lifetime, full.Revision)
	require.NoError(t, err)
	require.False(t, changed.Unchanged)
	require.Contains(t, changed.Entries, activeKey("changed"))
	require.Greater(t, changed.Revision, full.Revision)
	require.Positive(t, source.visits)
	// Same store and numeric revision, but a fresh serving lifetime.
	replacement, err := newParticipantAuthority(ctx, "authority", inventory, source, participantSnapshotLimits{8, 8192}, 1)
	require.NoError(t, err)
	defer replacement.stop(ctx)
	restarted, err := replacement.snapshotForPackageSince(ctx, request, "one", changed.Lifetime, changed.Revision)
	require.NoError(t, err)
	require.NotEqual(t, changed.Lifetime, restarted.Lifetime)
	require.Equal(t, changed.Revision, restarted.Revision)
	require.False(t, restarted.Unchanged)
	require.NotEmpty(t, restarted.Entries)
}

func TestParticipantConditionalCannotBypassRetirementOrReplacement(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	authority, err := newParticipantAuthority(ctx, "authority", inventory, engine, participantSnapshotLimits{8, 8192}, 1)
	require.NoError(t, err)
	defer authority.stop(ctx)
	request := participantRequestPackage(t, 1, "one")
	defer relay.ReleasePackage(request)
	full, err := authority.snapshotForPackage(ctx, request, "one")
	require.NoError(t, err)
	require.NoError(t, inventory.retire(ctx, "client", "one"))
	snapshot, err := authority.snapshotForPackageSince(ctx, request, "one", full.Lifetime, full.Revision)
	require.ErrorIs(t, err, ErrParticipantRetired)
	require.Nil(t, snapshot)
	require.NoError(t, inventory.replace(ctx, "client", "one", "two"))
	snapshot, err = authority.snapshotForPackageSince(ctx, request, "one", full.Lifetime, full.Revision)
	require.ErrorIs(t, err, ErrParticipantIncarnationConflict)
	require.Nil(t, snapshot)
}

type invalidConditionalSource struct {
	participantSnapshotSource
	visit    bool
	revision uint64
}

func (s invalidConditionalSource) ScanAtIndexSince(prefix string, known uint64, visit func(kvapi.Entry) bool) (uint64, bool, error) {
	if s.visit {
		_, err := s.ScanAtIndex(prefix, visit)
		if err != nil {
			return 0, false, err
		}
	}
	return s.revision, true, nil
}
func TestParticipantConditionalRejectsInconsistentSourceResult(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	require.NoError(t, inventory.enroll(ctx, "client", "one"))
	full, err := inventory.captureSnapshot(ctx, engine, "client", "one", participantSnapshotLimits{8, 8192})
	require.NoError(t, err)
	for _, source := range []invalidConditionalSource{{engine, true, full.Revision}, {engine, false, full.Revision + 1}} {
		snapshot, err := inventory.captureSnapshotSince(ctx, source, "client", "one", participantSnapshotLimits{8, 8192}, full.Revision)
		require.Error(t, err)
		require.Nil(t, snapshot)
	}
}
