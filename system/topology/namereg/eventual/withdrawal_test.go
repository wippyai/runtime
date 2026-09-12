// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/topology"
)

func TestWithdrawLocalPreservesRemoteAndRejectsOwnEcho(t *testing.T) {
	guard := &topology.NameGuard{}
	s := NewService(Config{LocalNodeID: "local", NameGuard: guard})
	local := makePID("local", "host", "old")
	remote := makePID("remote", "host", "owner")
	_, err := s.Register("shared", local)
	require.NoError(t, err)
	s.applyIncoming(&Entry{Name: "shared", PID: remote, Counter: 1, Priority: 100}, "remote")
	require.NoError(t, s.WithdrawLocal(context.Background()))
	require.Empty(t, s.owned)
	got, found := s.state.Lookup("shared")
	require.True(t, found)
	require.True(t, got.Equal(remote))
	_, err = s.Register("new", local)
	require.ErrorIs(t, err, topology.ErrNameAdmissionClosed)
	// A delayed own-origin echo can have a counter above the withdrawal dot.
	// It must become a tombstone, including for a name absent during the sweep.
	for _, name := range []string{"shared", "late"} {
		echo := &Entry{Name: name, PID: local, Counter: 1000, Priority: 200}
		s.applyIncoming(echo, "local")
		require.False(t, echo.Deleted, "normalization must not mutate the caller's entry")
	}
	_, found = s.state.Lookup("late")
	require.False(t, found)
	got, found = s.state.Lookup("shared")
	require.True(t, found)
	require.True(t, got.Equal(remote))
	require.NoError(t, s.WithdrawLocal(context.Background()))
	// Inspect replication state, not just visible winner suppression.
	for _, name := range []string{"shared", "late"} {
		entries := s.state.ShardEntries(ShardFor(name))
		tombstone := false
		for _, entry := range entries {
			if entry.Name == name && entry.Node == s.state.LocalNode() {
				tombstone = entry.Deleted
			}
		}
		require.True(t, tombstone)
	}
}

func TestWithdrawLocalCanceledJoinStaysClosed(t *testing.T) {
	guard := &topology.NameGuard{}
	s := NewService(Config{LocalNodeID: "local", NameGuard: guard})
	release, err := guard.LockContext(context.Background(), "held")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, s.WithdrawLocal(ctx), context.Canceled)
	release()
	require.NoError(t, s.WithdrawLocal(context.Background()))
	_, err = s.Register("late", makePID("local", "host", "p"))
	require.ErrorIs(t, err, topology.ErrNameAdmissionClosed)
}

func TestWithdrawalTombstonesReplicateAndDominateDelayedLiveFrames(t *testing.T) {
	source := NewService(Config{LocalNodeID: "source", NameGuard: &topology.NameGuard{}})
	peer := NewService(Config{LocalNodeID: "peer"})
	owner := makePID("source", "host", "p")
	_, err := source.Register("file-service", owner)
	require.NoError(t, err)
	liveFrames := source.DrainBroadcasts(0, 1<<20)
	require.NotEmpty(t, liveFrames)
	for _, frame := range liveFrames {
		peer.OnFrame(frame)
	}
	_, found := peer.state.Lookup("file-service")
	require.True(t, found)
	require.NoError(t, source.WithdrawLocal(context.Background()))
	tombstones := source.DrainBroadcasts(0, 1<<20)
	require.NotEmpty(t, tombstones)
	for _, frame := range tombstones {
		peer.OnFrame(frame)
	}
	for _, frame := range liveFrames {
		peer.OnFrame(frame)
	}
	_, found = peer.state.Lookup("file-service")
	require.False(t, found, "delayed live frame resurrected withdrawn binding")
}
