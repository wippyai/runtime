// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"testing"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func TestStrongOutcomeLookupSurvivesSameNameReuse(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	owner := mkPID("node", "owner")
	for _, attempt := range []string{"old", "new"} {
		pending, result := reserveOutcomeForTest(t, engine, attempt)
		reason := strongRejectConflict
		var missing []string
		if attempt == "new" {
			reason = "deadline"
			missing = []string{"b"}
		}
		ops, err := completeStrongResultOps(pending, result, strongResultExpired, reason, missing, 99)
		require.NoError(t, err)
		ops = append(ops, kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: pending.Key})
		committed, err := engine.Txn(ops)
		require.NoError(t, err)
		require.True(t, committed)
	}
	_, old, err := readStrongOutcome(engine.Get, "old", "claim", owner.String(), 1024)
	require.NoError(t, err)
	require.Equal(t, strongRejectConflict, old.Reason)
	_, next, err := readStrongOutcome(engine.Get, "new", "claim", owner.String(), 1024)
	require.NoError(t, err)
	require.Equal(t, "deadline", next.Reason)
	require.Equal(t, []string{"b"}, next.Missing)
	_, _, err = readStrongOutcome(engine.Get, "old", "another-name", owner.String(), 1024)
	require.Error(t, err)
	_, _, err = readStrongOutcome(engine.Get, "absent", "claim", owner.String(), 1024)
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound, "absence is not a committed timeout")
}

func TestStrongResultRetentionCannotExpireClaim(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	pending, result := reserveOutcomeForTest(t, engine)
	ops, err := expiredStrongResultOps(engine.Get, result, int64(^uint64(0)>>1), 1024)
	require.NoError(t, err)
	require.Empty(t, ops, "pending result is never time-reclaimed")
	ops, err = completeStrongResultOps(pending, result, strongResultActive, "", nil, 99)
	require.NoError(t, err)
	ops = append(ops, kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: pending.Key}, kvapi.TxnOp{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: activeKey("claim"), Value: []byte("live-claim")})
	committed, err := engine.Txn(ops)
	require.NoError(t, err)
	require.True(t, committed)
	terminal, err := engine.Get(result.Key)
	require.NoError(t, err)
	ops, err = expiredStrongResultOps(engine.Get, terminal, 98, 1024)
	require.NoError(t, err)
	require.Empty(t, ops)
	ops, err = expiredStrongResultOps(engine.Get, terminal, 99, 1024)
	require.NoError(t, err)
	require.NotEmpty(t, ops)
	committed, err = engine.Txn(ops)
	require.NoError(t, err)
	require.True(t, committed)
	_, err = engine.Get(result.Key)
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	active, err := engine.Get(activeKey("claim"))
	require.NoError(t, err)
	require.Equal(t, []byte("live-claim"), active.Value, "result reclamation is not claim expiration")
	usage, _, err := readStrongResultUsage(engine.Get)
	require.NoError(t, err)
	require.Equal(t, strongResultUsage{}, usage)
}
