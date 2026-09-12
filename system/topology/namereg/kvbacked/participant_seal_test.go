// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

func TestParticipantSealJoinsMutationsWithoutCancelingReconciliation(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	service := NewService(engine, "member", nil, nil)
	service.ConfigureStrong(StrongDeps{Incarnation: "one"})
	service.strong.participants = inventory
	ctx := context.Background()
	limits := participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}
	require.NoError(t, service.startParticipantMember(ctx, inventory, engine, limits))
	t.Cleanup(func() { require.NoError(t, service.StopReconciler(ctx)) })
	leave, err := service.admitMutation(ctx)
	require.NoError(t, err)
	defer func() {
		if leave != nil {
			leave()
		}
	}()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, service.sealParticipantMutations(canceled), context.Canceled)
	require.False(t, service.NameReady())
	run := service.reconciler.Load()
	require.NoError(t, run.ctx.Err(), "withdrawal must retain reconciliation")
	select {
	case <-run.mutationsDrained:
		t.Fatal("seal completed while an admitted mutation was held")
	default:
	}
	_, err = service.RegisterScope(ctx, "late", mkPID("member", "owner"), globalapi.Consistent)
	require.ErrorIs(t, err, context.Canceled)
	leave()
	leave = nil
	require.NoError(t, service.sealParticipantMutations(ctx))
	require.NoError(t, service.sealParticipantMutations(ctx))
	// Refresh/recovery may update the synchronization flag, but cannot reverse
	// terminal application admission on this lifecycle.
	_, err = service.refreshParticipant(ctx, inventory, engine, limits)
	require.NoError(t, err)
	service.ready.Store(true)
	require.False(t, service.NameReady())
	require.NoError(t, run.ctx.Err())
	require.NoError(t, service.StopReconciler(ctx))
}
