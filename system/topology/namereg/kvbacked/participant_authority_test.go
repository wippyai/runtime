// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

type gatedParticipantAuthoritySource struct {
	participantSnapshotSource
	entered chan struct{}
	release chan struct{}
}

func (s *gatedParticipantAuthoritySource) ScanAtIndex(prefix string, visit func(kvapi.Entry) bool) (uint64, error) {
	close(s.entered)
	<-s.release
	return s.participantSnapshotSource.ScanAtIndex(prefix, visit)
}

func TestParticipantAuthorityRejectsPeerMismatchBeforeEnrollment(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	authority, err := newParticipantAuthority(context.Background(), "authority", inventory, engine, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}, 1)
	require.NoError(t, err)
	defer authority.stop(context.Background())
	for _, peer := range []string{"", "attacker"} {
		pkg := relay.NewServicePackage("victim", RegistryHostID, "authority", RegistryHostID, "snapshot")
		pkg.ReceivedFrom = peer
		snapshot, err := authority.snapshotForPackage(context.Background(), pkg, "incarnation")
		relay.ReleasePackage(pkg)
		require.ErrorIs(t, err, errParticipantPeerMismatch)
		require.Nil(t, snapshot)
	}
	_, _, err = inventory.readSnapshot()
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	pkg := relay.NewServicePackage("client", RegistryHostID, "authority", RegistryHostID, "snapshot")
	defer relay.ReleasePackage(pkg)
	pkg.ReceivedFrom = "client"
	snapshot, err := authority.snapshotForPackage(context.Background(), pkg, "one")
	require.NoError(t, err)
	require.NotZero(t, snapshot.Revision)
	members, _, err := inventory.readSnapshot()
	require.NoError(t, err)
	require.Equal(t, "one", members["client"])
}

func TestParticipantAuthorityBoundsWorkBeforeEnrollmentAndJoinsStop(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	source := &gatedParticipantAuthoritySource{participantSnapshotSource: engine, entered: make(chan struct{}), release: make(chan struct{})}
	authority, err := newParticipantAuthority(context.Background(), "authority", inventory, source, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}, 1)
	require.NoError(t, err)
	pkg := relay.NewServicePackage("client", RegistryHostID, "authority", RegistryHostID, "snapshot")
	pkg.ReceivedFrom = "client"
	result := make(chan error, 1)
	go func() {
		defer relay.ReleasePackage(pkg)
		_, err := authority.snapshotForPackage(context.Background(), pkg, "one")
		result <- err
	}()
	select {
	case <-source.entered:
	case <-time.After(time.Second):
		t.Fatal("capture did not start")
	}
	second := relay.NewServicePackage("second", RegistryHostID, "authority", RegistryHostID, "snapshot")
	defer relay.ReleasePackage(second)
	second.ReceivedFrom = "second"
	_, err = authority.snapshotForPackage(context.Background(), second, "two")
	require.ErrorIs(t, err, errParticipantAuthorityBusy)
	members, _, err := inventory.readSnapshot()
	require.NoError(t, err)
	require.NotContains(t, members, "second", "rejected capacity admission must not enroll another node")
	deadline, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, authority.stop(deadline), context.DeadlineExceeded)
	close(source.release)
	joined, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	require.NoError(t, authority.stop(joined))
	require.ErrorIs(t, <-result, context.Canceled)
	_, err = authority.snapshotForPackage(context.Background(), second, "two")
	require.ErrorIs(t, err, context.Canceled)
}
