// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParticipantMemberBootstrapFailureCannotOpenAdmission(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	service := NewService(engine, "member", nil, nil)
	service.ConfigureStrong(StrongDeps{Incarnation: "one"})
	service.strong.participants = inventory
	ctx := context.Background()
	require.ErrorContains(t, service.StartReconciler(ctx), "bootstrap")
	require.False(t, service.NameReady())
	cause := errors.New("authority lost")
	limits := participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}
	require.ErrorIs(t, service.startParticipantMember(ctx, inventory, failedParticipantSnapshotSource{cause}, limits), cause)
	require.False(t, service.NameReady())
	require.Nil(t, service.reconciler.Load())
	members, _, err := inventory.readSnapshot()
	require.NoError(t, err)
	require.Equal(t, "one", members["member"], "failed bootstrap does not retire committed enrollment")
	require.NoError(t, service.startParticipantMember(ctx, inventory, engine, limits))
	t.Cleanup(func() { require.NoError(t, service.StopReconciler(ctx)) })
	require.True(t, service.NameReady())
	require.NoError(t, service.StopReconciler(ctx))
	require.False(t, service.NameReady())
	require.Error(t, service.startParticipantMember(ctx, inventory, engine, limits))
}
