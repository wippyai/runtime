// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func TestConfigureParticipationValidatesBeforeMutation(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	service := NewService(engine, "member", nil, nil)
	require.Error(t, service.ConfigureParticipation(4))
	service.ConfigureStrong(StrongDeps{})
	require.Error(t, service.ConfigureParticipation(4))
	require.Nil(t, service.strong.participants)
	service.ConfigureStrong(StrongDeps{Incarnation: "one"})
	require.Error(t, service.ConfigureParticipation(0))
	require.Nil(t, service.strong.participants)
	require.NoError(t, service.ConfigureParticipation(4))
	inventory := service.strong.participants
	require.NotNil(t, inventory)
	require.Error(t, service.ConfigureParticipation(8))
	require.Same(t, inventory, service.strong.participants)
	_, _, err := inventory.readSnapshot()
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound, "configuration must not enroll")
	ctx := context.Background()
	require.NoError(t, service.startParticipantMember(ctx, inventory, engine, participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}))
	t.Cleanup(func() { require.NoError(t, service.StopReconciler(ctx)) })
	require.Error(t, service.ConfigureParticipation(8))
	require.NoError(t, service.StopReconciler(ctx))
	require.Error(t, service.ConfigureParticipation(8))
}
