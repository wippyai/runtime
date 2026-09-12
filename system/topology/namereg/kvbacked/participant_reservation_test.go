// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func TestParticipantReservationCannotCommitStaleInventory(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	require.NoError(t, inventory.enroll(ctx, "node-1", "incarnation-1"))
	owner := mkPID("node-1", "owner")
	original := pendingHeader{Name: "claim", PID: owner.String()}
	ops, err := inventory.reservationOps(ctx, original)
	require.NoError(t, err)
	// A join between inventory read and reservation commit must invalidate the
	// entire transaction. Neither pending nor active state may be written.
	require.NoError(t, inventory.enroll(ctx, "node-2", "incarnation-2"))
	committed, err := engine.Txn(ops)
	require.NoError(t, err)
	require.False(t, committed)
	_, err = engine.Get(pendingKey("claim"))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	_, err = engine.Get(activeKey("claim"))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)

	ops, err = inventory.reservationOps(ctx, original)
	require.NoError(t, err)
	committed, err = engine.Txn(ops)
	require.NoError(t, err)
	require.True(t, committed)
	entry, err := engine.Get(pendingKey("claim"))
	require.NoError(t, err)
	var stored pendingHeader
	require.NoError(t, decodeInto(entry.Value, &stored))
	require.Equal(t, []string{"node-1", "node-2"}, stored.RequiredNodes)
	require.Equal(t, map[string]string{"node-1": "incarnation-1", "node-2": "incarnation-2"}, stored.RequiredIncarnations)
	require.Equal(t, owner.Node, stored.NodeID)
	_, err = decodePending(entry.Value)
	require.NoError(t, err)

}

func TestParticipantReservationRequiresEnrolledOwnerAndLiveContext(t *testing.T) {
	inventory, _ := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	owner := mkPID("node-1", "owner")
	hdr := pendingHeader{Name: "claim", PID: owner.String()}
	ops, err := inventory.reservationOps(ctx, hdr)
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	require.Nil(t, ops)
	require.NoError(t, inventory.enroll(ctx, "node-2", "incarnation-2"))
	ops, err = inventory.reservationOps(ctx, hdr)
	require.Error(t, err)
	require.Nil(t, ops)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	ops, err = inventory.reservationOps(canceled, hdr)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, ops)
}
