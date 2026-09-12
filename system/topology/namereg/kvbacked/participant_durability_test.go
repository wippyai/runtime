// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/cluster/clustertest"
)

// This tests committed inventory durability with real Raft/disk replay. It
// deliberately does not infer authorization to replace a crashed process from
// a successful Raft restart, nor claim independent-process/native transport.
func TestParticipantRetirementSurvivesFullRaftRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("real Raft disk-recovery integration")
	}
	c := clustertest.NewCluster(t, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	inventoryFor := func(node *clustertest.Node) *participantInventory {
		t.Helper()
		inventory, err := newParticipantInventory(node.KV, node.KV.GetLinearizable, 4)
		require.NoError(t, err)
		return inventory
	}
	inventory := inventoryFor(c.Leader())
	require.NoError(t, inventory.enroll(ctx, "participant", "first"))
	_, staleCheck, err := inventory.readSnapshot()
	require.NoError(t, err)
	require.NoError(t, inventory.retire(ctx, "participant", "first"))
	require.NoError(t, inventory.replace(ctx, "participant", "first", "second"))
	require.NoError(t, inventory.retire(ctx, "participant", "second"))
	// Ensure all replicas have applied before stopping every Raft member. Their
	// only retained authority across the rebuild is their on-disk Raft state.
	require.Eventually(t, func() bool {
		for _, node := range c.Nodes() {
			entry, err := node.KV.Get(retiredParticipantsKey)
			if err != nil {
				return false
			}
			retired, err := decodeParticipantRecord(entry.Value)
			if err != nil || retired["participant"] != "second" {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)
	for i := range c.Nodes() {
		c.Kill(i)
	}
	for i := range c.Nodes() {
		c.Restart(i)
	}
	leader := c.WaitLeader(10 * time.Second)
	require.NotNil(t, leader)
	inventory = inventoryFor(leader)
	active, _, err := inventory.readSnapshot()
	require.NoError(t, err)
	require.NotContains(t, active, "participant")
	for _, incarnation := range []string{"first", "second", "third"} {
		require.ErrorIs(t, inventory.enroll(ctx, "participant", incarnation), ErrParticipantRetired)
	}
	require.ErrorIs(t, inventory.replace(ctx, "participant", "first", "third"), ErrParticipantIncarnationConflict)
	committed, err := leader.KV.Txn([]kvapi.TxnOp{staleCheck, {Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: pendingKey("stale-after-restart"), Value: []byte("invalid")}})
	require.NoError(t, err)
	require.False(t, committed, "pre-retirement inventory version survived disk replay as a valid reservation check")
	require.NoError(t, inventory.replace(ctx, "participant", "second", "third"))
	require.ErrorIs(t, inventory.retire(ctx, "participant", "second"), ErrParticipantIncarnationConflict)
	active, _, err = inventory.readSnapshot()
	require.NoError(t, err)
	require.Equal(t, "third", active["participant"])
}
