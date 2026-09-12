// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"go.uber.org/zap"
)

func TestStrongResultSweepBatchesWithoutDeletingClaims(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	for _, attempt := range []string{"one", "two"} {
		pending, result := reserveOutcomeForTest(t, engine, attempt)
		ops, err := completeStrongResultOps(pending, result, strongResultActive, "", nil, 99)
		require.NoError(t, err)
		ops = append(ops, kvapi.TxnOp{Kind: kvapi.TxnDelete, Key: pending.Key})
		committed, err := engine.Txn(ops)
		require.NoError(t, err)
		require.True(t, committed)
	}
	_, err := engine.Set(activeKey("claim"), []byte("still-owned"))
	require.NoError(t, err)
	svc := NewService(engine, "node", nil, zap.NewNop())
	leader := false
	svc.ConfigureStrong(StrongDeps{IsLeader: func() bool { return leader }, Clock: func() time.Time { return time.Unix(0, 100) }})
	policy := DefaultStrongResultPolicy()
	policy.ReclaimBatch = 1
	require.NoError(t, svc.ConfigureStrongResults(policy))
	require.NoError(t, svc.strong.reclaimStrongResults(context.Background()))
	usage, _, err := readStrongResultUsage(engine.Get)
	require.NoError(t, err)
	require.Equal(t, uint64(2), usage.Entries, "followers must not reclaim")
	leader = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, svc.strong.reclaimStrongResults(ctx), context.Canceled)
	for _, remaining := range []uint64{1, 0} {
		require.NoError(t, svc.strong.reclaimStrongResults(context.Background()))
		usage, _, err = readStrongResultUsage(engine.Get)
		require.NoError(t, err)
		require.Equal(t, remaining, usage.Entries)
		require.Equal(t, remaining*1024, usage.Bytes)
	}
	require.NoError(t, svc.strong.reclaimStrongResults(context.Background()))
	active, err := engine.Get(activeKey("claim"))
	require.NoError(t, err)
	require.Equal(t, []byte("still-owned"), active.Value)
	// Even with a very old pending entry, GC cannot treat age as termination.
	_, result := reserveOutcomeForTest(t, engine, "pending")
	require.NoError(t, svc.strong.reclaimStrongResults(context.Background()))
	retained, err := engine.Get(result.Key)
	require.NoError(t, err)
	require.Equal(t, result.Value, retained.Value)
}

type resultGCConcurrentWrite struct {
	kvapi.Engine
	before func()
}

func (e *resultGCConcurrentWrite) Txn(ops []kvapi.TxnOp) (bool, error) {
	if e.before != nil {
		f := e.before
		e.before = nil
		f()
	}
	return e.Engine.Txn(ops)
}

func TestStrongResultSweepQuotaRaceIsAtomic(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	pending, result := reserveOutcomeForTest(t, engine)
	ops, err := completeStrongResultOps(pending, result, strongResultActive, "", nil, 99)
	require.NoError(t, err)
	ops = append(ops, kvapi.TxnOp{Kind: kvapi.TxnDelete, Key: pending.Key})
	committed, err := engine.Txn(ops)
	require.NoError(t, err)
	require.True(t, committed)
	wrapped := &resultGCConcurrentWrite{Engine: engine}
	wrapped.before = func() {
		// Admit another attempt after GC has scanned but before its quota CAS.
		reserveOutcomeForTest(t, engine, "concurrent")
	}
	svc := NewService(wrapped, "node", nil, zap.NewNop())
	svc.ConfigureStrong(StrongDeps{Clock: func() time.Time { return time.Unix(0, 100) }})
	require.NoError(t, svc.strong.reclaimStrongResults(context.Background()))
	_, err = engine.Get(result.Key)
	require.NoError(t, err, "a failed quota CAS cannot delete terminal evidence")
	usage, _, err := readStrongResultUsage(engine.Get)
	require.NoError(t, err)
	require.Equal(t, uint64(2), usage.Entries)
	require.NoError(t, svc.strong.reclaimStrongResults(context.Background()))
	_, err = engine.Get(result.Key)
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	usage, _, err = readStrongResultUsage(engine.Get)
	require.NoError(t, err)
	require.Equal(t, uint64(1), usage.Entries)
	_, err = engine.Get(strongResultKey("concurrent"))
	require.NoError(t, err, "pending concurrent admission survives reclamation")
}
