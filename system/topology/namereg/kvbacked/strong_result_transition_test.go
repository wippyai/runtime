// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"testing"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func reserveOutcomeForTest(t *testing.T, engine kvapi.Engine, attempts ...string) (kvapi.Entry, kvapi.Entry) {
	t.Helper()
	attempt := "attempt"
	if len(attempts) > 0 {
		attempt = attempts[0]
	}
	owner := mkPID("node", "owner")
	hdr := pendingHeader{AttemptID: attempt, Name: "claim", PID: owner.String(), RequiredNodes: []string{"a", "b"}}
	header, err := encode(hdr)
	require.NoError(t, err)
	initial, err := encode(strongOutcomeRecord{AttemptID: hdr.AttemptID, Name: hdr.Name, PID: hdr.PID})
	require.NoError(t, err)
	ops, err := reserveStrongResultOps(engine.Get, strongResultLimits{Entries: 2, Bytes: 2048}, hdr.AttemptID, 1024, initial)
	require.NoError(t, err)
	ops = append(ops, kvapi.TxnOp{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: pendingKey(hdr.Name), Value: header})
	committed, err := engine.Txn(ops)
	require.NoError(t, err)
	require.True(t, committed)
	pending, err := engine.Get(pendingKey(hdr.Name))
	require.NoError(t, err)
	result, err := engine.Get(strongResultKey(hdr.AttemptID))
	require.NoError(t, err)
	return pending, result
}

func TestStrongResultEvidenceCommitsWithPendingDeletion(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	pending, result := reserveOutcomeForTest(t, engine)
	// Local test engine has zero epochs. Supply a shared original commit epoch
	// to prove it survives serialization independently of the rewritten entry.
	pending.Epoch, result.Epoch = 41, 41
	ops, err := completeStrongResultOps(pending, result, strongResultExpired, "deadline", []string{"b", "a"}, 99)
	require.NoError(t, err)
	ops = append(ops, kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: pending.Key})
	committed, err := engine.Txn(ops)
	require.NoError(t, err)
	require.True(t, committed)
	_, err = engine.Get(pending.Key)
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	stored, err := engine.Get(result.Key)
	require.NoError(t, err)
	var reservation strongResultReservation
	require.NoError(t, decodeInto(stored.Value, &reservation))
	var outcome strongOutcomeRecord
	require.NoError(t, decodeInto(reservation.Data, &outcome))
	require.Equal(t, uint64(41), outcome.PendingEpoch)
	require.Equal(t, "attempt", outcome.AttemptID)
	require.Equal(t, []string{"a", "b"}, outcome.Missing)
	require.Equal(t, int64(99), outcome.RetainUntil)
	usage, _, err := readStrongResultUsage(engine.Get)
	require.NoError(t, err)
	require.Equal(t, uint64(1024), usage.Bytes, "completion cannot acquire additional capacity")
}

func TestStrongResultStaleDecisionCannotAffectReplacement(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	pending, result := reserveOutcomeForTest(t, engine)
	ops, err := completeStrongResultOps(pending, result, strongResultActive, "", nil, 99)
	require.NoError(t, err)
	ops = append(ops, kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: pending.Key})
	_, err = engine.Set(pending.Key, []byte("replacement"))
	require.NoError(t, err)
	committed, err := engine.Txn(ops)
	require.NoError(t, err)
	require.False(t, committed)
	stillPending, err := engine.Get(result.Key)
	require.NoError(t, err)
	require.Equal(t, result.Value, stillPending.Value)
	replacement, err := engine.Get(pending.Key)
	require.NoError(t, err)
	require.Equal(t, []byte("replacement"), replacement.Value)
}

func TestStrongResultRejectsUnrelatedEvidence(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	pending, result := reserveOutcomeForTest(t, engine)
	_, err := completeStrongResultOps(pending, result, strongResultExpired, "deadline", []string{"foreign"}, 99)
	require.Error(t, err)
	_, err = completeStrongResultOps(pending, result, strongResultActive, "deadline", []string{"a"}, 99)
	require.Error(t, err)
	_, err = completeStrongResultOps(pending, result, strongResultExpired, "deadline", nil, 99)
	require.Error(t, err)
	result.Epoch++
	_, err = completeStrongResultOps(pending, result, strongResultActive, "", nil, 99)
	require.Error(t, err)
}

func TestStrongResultAdmissionBudgetFitsEveryTerminalForm(t *testing.T) {
	for _, participants := range [][]string{{"one"}, {"alpha", "β-node", "longer-node-identifier"}} {
		owner := mkPID("node", "owner")
		header := pendingHeader{AttemptID: "attempt", Name: "claim", PID: owner.String(), RequiredNodes: participants}
		capacity, initial, err := planStrongResultCapacity(header, 4096)
		require.NoError(t, err)
		require.Less(t, capacity, uint64(4096))
		for _, phase := range []strongResultPhase{strongResultActive, strongResultExpired} {
			for _, reason := range []string{"deadline", strongRejectConflict, "unreserve"} {
				_, engine := newParticipantTestInventory(t, 4)
				ops, err := reserveStrongResultOps(engine.Get, strongResultLimits{Entries: 1, Bytes: capacity}, header.AttemptID, capacity, initial)
				require.NoError(t, err)
				pendingBody, err := encode(header)
				require.NoError(t, err)
				ops = append(ops, kvapi.TxnOp{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: pendingKey(header.Name), Value: pendingBody})
				committed, err := engine.Txn(ops)
				require.NoError(t, err)
				require.True(t, committed)
				pending, err := engine.Get(pendingKey(header.Name))
				require.NoError(t, err)
				result, err := engine.Get(strongResultKey(header.AttemptID))
				require.NoError(t, err)
				pending.Epoch, result.Epoch = ^uint64(0), ^uint64(0)
				missing := participants
				terminalReason := reason
				if phase == strongResultActive {
					missing = nil
					terminalReason = ""
				}
				transition, err := completeStrongResultOps(pending, result, phase, terminalReason, missing, int64(^uint64(0)>>1))
				require.NoError(t, err, "admitted capacity must cover terminal evidence")
				require.LessOrEqual(t, uint64(len(result.Key)+len(transition[1].Value)), capacity)
			}
		}
		_, _, err = planStrongResultCapacity(header, capacity-1)
		require.ErrorIs(t, err, errStrongResultTooLarge)
	}
}

// A participant-set rewrite changes the pending key's commit index, not the
// identity of the admitted attempt or its originally reserved result slot.
func TestStrongResultPendingRewriteRetainsOriginalEpoch(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	pending, result := reserveOutcomeForTest(t, engine)
	stale := pending
	header, err := decodePending(pending.Value)
	require.NoError(t, err)
	header.RequiredNodes = []string{"a"}
	body, err := encode(header)
	require.NoError(t, err)
	_, err = engine.Set(pending.Key, body)
	require.NoError(t, err)
	pending, err = engine.Get(pending.Key)
	require.NoError(t, err)
	// The local engine supplies zero epochs; model the distinct Raft indices.
	stale.Epoch, result.Epoch, pending.Epoch = 41, 41, 52
	oldOps, err := completeStrongResultOps(stale, result, strongResultActive, "", nil, 99)
	require.NoError(t, err)
	committed, err := engine.Txn(oldOps)
	require.NoError(t, err)
	require.False(t, committed, "the old pending version cannot complete after rewrite")
	ops, err := completeStrongResultOps(pending, result, strongResultExpired, "deadline", []string{"a"}, 99)
	require.NoError(t, err)
	ops = append(ops, kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: pending.Key})
	committed, err = engine.Txn(ops)
	require.NoError(t, err)
	require.True(t, committed)
	stored, err := engine.Get(result.Key)
	require.NoError(t, err)
	outcome, err := decodeStrongOutcome(stored, 1024)
	require.NoError(t, err)
	require.Equal(t, uint64(41), outcome.PendingEpoch)
	require.Equal(t, []string{"a"}, outcome.Missing)
	require.Equal(t, header.AttemptID, outcome.AttemptID)
}
