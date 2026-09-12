// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"testing"
)

func TestStrongResultCapacityCommitsWithPending(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	limits := strongResultLimits{Entries: 1, Bytes: 256}
	first, err := reserveStrongResultOps(engine.Get, limits, "first", 256, nil)
	require.NoError(t, err)
	second, err := reserveStrongResultOps(engine.Get, limits, "second", 256, nil)
	require.NoError(t, err)
	first = append(first, kvapi.TxnOp{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: pendingKey("one"), Value: []byte("one")})
	committed, err := engine.Txn(first)
	require.NoError(t, err)
	require.True(t, committed)
	second = append(second, kvapi.TxnOp{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: pendingKey("two"), Value: []byte("two")})
	committed, err = engine.Txn(second)
	require.NoError(t, err)
	require.False(t, committed, "stale usage must not oversubscribe capacity")
	_, err = engine.Get(pendingKey("two"))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	_, err = reserveStrongResultOps(engine.Get, limits, "third", 256, nil)
	require.ErrorIs(t, err, errStrongResultCapacity)
}

func TestStrongResultFailedClaimDoesNotConsumeCapacity(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	_, err := engine.Set(pendingKey("claim"), []byte("existing"))
	require.NoError(t, err)
	ops, err := reserveStrongResultOps(engine.Get, strongResultLimits{Entries: 1, Bytes: 256}, "attempt", 256, nil)
	require.NoError(t, err)
	ops = append(ops, kvapi.TxnOp{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: pendingKey("claim"), Value: []byte("replacement")})
	committed, err := engine.Txn(ops)
	require.NoError(t, err)
	require.False(t, committed)
	_, err = engine.Get(strongResultUsageKey)
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	_, err = engine.Get(strongResultKey("attempt"))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
}

func TestStrongResultReleaseFencesVersionAndCapacity(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	limits := strongResultLimits{Entries: 1, Bytes: 256}
	ops, err := reserveStrongResultOps(engine.Get, limits, "attempt", 256, nil)
	require.NoError(t, err)
	committed, err := engine.Txn(ops)
	require.NoError(t, err)
	require.True(t, committed)
	entry, err := engine.Get(strongResultKey("attempt"))
	require.NoError(t, err)
	stale, err := releaseStrongResultOps(engine.Get, entry)
	require.NoError(t, err)
	// Model the pending -> terminal rewrite under its reserved budget.
	value, err := encode(strongResultReservation{Capacity: 256, Data: []byte("terminal")})
	require.NoError(t, err)
	_, err = engine.Set(entry.Key, value)
	require.NoError(t, err)
	committed, err = engine.Txn(stale)
	require.NoError(t, err)
	require.False(t, committed)
	usage, _, err := readStrongResultUsage(engine.Get)
	require.NoError(t, err)
	require.Equal(t, uint64(256), usage.Bytes)
	entry, err = engine.Get(entry.Key)
	require.NoError(t, err)
	current, err := releaseStrongResultOps(engine.Get, entry)
	require.NoError(t, err)
	committed, err = engine.Txn(current)
	require.NoError(t, err)
	require.True(t, committed)
	committed, err = engine.Txn(current)
	require.NoError(t, err)
	require.False(t, committed, "duplicate release cannot free another reservation")
	usage, _, err = readStrongResultUsage(engine.Get)
	require.NoError(t, err)
	require.Equal(t, strongResultUsage{}, usage)
	_, err = reserveStrongResultOps(engine.Get, limits, "next", 256, nil)
	require.NoError(t, err)
}
