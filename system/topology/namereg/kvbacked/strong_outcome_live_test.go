// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

type outcomeCommitObserver struct {
	kvapi.Engine
	after func()
}

func (e *outcomeCommitObserver) Txn(ops []kvapi.TxnOp) (bool, error) {
	committed, err := e.Engine.Txn(ops)
	if committed && err == nil && e.after != nil {
		for _, op := range ops {
			if op.Kind == kvapi.TxnDelete && op.Key == pendingKey("claim") {
				after := e.after
				e.after = nil
				after()
				break
			}
		}
	}
	return committed, err
}

func TestStrongOutcomePublishedBeforeCompletionReturns(t *testing.T) {
	r := newStrongReg(t, []string{"node-1", "missing"}, time.Second, nil)
	owner := mkPID("node-1", "owner")
	ctx := context.Background()
	header := pendingHeader{AttemptID: "first", Name: "claim", PID: owner.String(), NodeID: owner.Node}
	committed, err := r.strong.createPending(ctx, header)
	require.NoError(t, err)
	require.True(t, committed)
	entry, err := r.engine.Get(pendingKey("claim"))
	require.NoError(t, err)
	header, err = decodePending(entry.Value)
	require.NoError(t, err)
	observed := false
	// Observe as a separate registry: no leader-local terminal maps or waiters.
	follower := NewService(r.engine, "follower", nil, nil)
	follower.ConfigureStrong(StrongDeps{IsLeader: func() bool { return false }})
	r.engine = &outcomeCommitObserver{Engine: r.engine, after: func() {
		observed = true
		_, err := follower.strong.attemptOutcome(ctx, "claim", owner, "first")
		var timeout *globalapi.StrongRegistrationTimeoutError
		require.ErrorAs(t, err, &timeout)
		require.Contains(t, timeout.MissingAcks, "missing")
	}}
	r.strong.leaderExpire("claim", entry.Epoch, entry.Version, header, "deadline")
	require.True(t, observed, "terminal evidence is readable before the winning transaction returns")
	// Reuse both name and owner; the old decision must remain independently readable.
	header.AttemptID = "second"
	committed, err = r.strong.createPending(ctx, header)
	require.NoError(t, err)
	require.True(t, committed)
	entry, err = r.engine.Get(pendingKey("claim"))
	require.NoError(t, err)
	header, err = decodePending(entry.Value)
	require.NoError(t, err)
	r.strong.leaderExpire("claim", entry.Epoch, entry.Version, header, "unreserve")
	_, err = follower.strong.attemptOutcome(ctx, "claim", owner, "second")
	require.ErrorIs(t, err, globalapi.ErrStrongRegistrationWithdrawn)
	_, err = follower.strong.attemptOutcome(ctx, "claim", owner, "first")
	require.ErrorIs(t, err, globalapi.ErrStrongRegistrationTimeout)
}

func TestStrongResultCapacityPreservesExistingClaimSemantics(t *testing.T) {
	r := newStrongReg(t, []string{"node-1"}, time.Second, nil)
	policy := DefaultStrongResultPolicy()
	policy.Entries, policy.ReclaimBatch = 1, 1
	require.NoError(t, r.ConfigureStrongResults(policy))
	ctx := context.Background()
	owner := mkPID("node-1", "owner")
	other := mkPID("node-1", "other")
	header, entry := seedStrongPending(t, r, pendingHeader{Name: "owned", PID: owner.String(), RequiredNodes: []string{"node-1"}})
	require.NoError(t, r.strong.attestHeader("owned", entry.Epoch, owner, header))
	r.strong.leaderPromote("owned", entry.Epoch, entry.Version, header)
	original, err := r.strong.attemptOutcome(ctx, "owned", owner, header.AttemptID)
	require.NoError(t, err)
	require.Equal(t, globalapi.RegisterStateActive, original.State)
	repeated, err := r.RegisterScope(ctx, "owned", owner, globalapi.Strong)
	require.NoError(t, err, "retained results must not break idempotent registration")
	require.Equal(t, original, repeated)
	_, err = r.RegisterScope(ctx, "owned", other, globalapi.Strong)
	require.ErrorIs(t, err, globalapi.ErrNameAlreadyRegistered)
	_, err = r.RegisterScope(ctx, "new", other, globalapi.Strong)
	require.ErrorIs(t, err, errStrongResultCapacity)
	var classified apierror.Error
	require.ErrorAs(t, err, &classified)
	require.Equal(t, apierror.RateLimited, classified.Kind())
	require.Equal(t, apierror.True, classified.Retryable())
	_, err = r.engine.Get(pendingKey("new"))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound, "failed admission cannot leave a pending name")
	usage, _, err := readStrongResultUsage(r.engine.Get)
	require.NoError(t, err)
	require.Equal(t, uint64(1), usage.Entries)
}

func TestStrongResultCapacityPreservesPendingClaimSemantics(t *testing.T) {
	r := newStrongReg(t, []string{"node-1", "missing"}, time.Second, nil)
	policy := DefaultStrongResultPolicy()
	policy.Entries, policy.ReclaimBatch = 1, 1
	require.NoError(t, r.ConfigureStrongResults(policy))
	ctx := context.Background()
	owner := mkPID("node-1", "owner")
	other := mkPID("node-1", "other")
	committed, err := r.strong.createPending(ctx, pendingHeader{AttemptID: "pending", Name: "owned", PID: owner.String(), NodeID: owner.Node})
	require.NoError(t, err)
	require.True(t, committed)
	entry, err := r.engine.Get(pendingKey("owned"))
	require.NoError(t, err)
	repeated, err := r.RegisterScope(ctx, "owned", owner, globalapi.Strong)
	require.NoError(t, err)
	require.Equal(t, owner, repeated.PID)
	require.Equal(t, entry.Epoch, repeated.Epoch)
	require.NotEqual(t, globalapi.RegisterStateActive, repeated.State, "pending retry cannot claim promotion")
	_, err = r.RegisterScope(ctx, "owned", other, globalapi.Strong)
	require.ErrorIs(t, err, globalapi.ErrPendingConflict)
	retained, err := r.engine.Get(pendingKey("owned"))
	require.NoError(t, err)
	require.Equal(t, entry, retained)
	usage, _, err := readStrongResultUsage(r.engine.Get)
	require.NoError(t, err)
	require.Equal(t, uint64(1), usage.Entries)
}

func TestStrongCompletionRefusesMissingAttemptEvidence(t *testing.T) {
	for _, attempt := range []string{"", "missing-result"} {
		t.Run(attempt, func(t *testing.T) {
			r := newStrongReg(t, []string{"node-1"}, time.Second, nil)
			owner := mkPID("node-1", "owner")
			header := pendingHeader{AttemptID: attempt, Name: "claim", PID: owner.String(), RequiredNodes: []string{"node-1"}}
			body, err := encode(header)
			require.NoError(t, err)
			_, err = r.engine.Set(pendingKey(header.Name), body)
			require.NoError(t, err)
			entry, err := r.engine.Get(pendingKey(header.Name))
			require.NoError(t, err)
			ops, err := r.strong.terminalResultOps(header.Name, entry.Epoch, entry.Version, header, strongResultExpired, "unreserve", nil)
			require.Error(t, err)
			require.Empty(t, ops)
			r.strong.leaderExpire(header.Name, entry.Epoch, entry.Version, header, "unreserve")
			retained, err := r.engine.Get(entry.Key)
			require.NoError(t, err)
			require.Equal(t, entry, retained, "invalid evidence cannot remove the reservation")
		})
	}
}

func TestStrongOversizedAdmissionIsNonRetryableAndWritesNothing(t *testing.T) {
	r := newStrongReg(t, []string{"node-1"}, time.Second, nil)
	policy := DefaultStrongResultPolicy()
	policy.RecordBytes = 1
	require.NoError(t, r.ConfigureStrongResults(policy))
	_, err := r.RegisterScope(context.Background(), "oversized", mkPID("node-1", "owner"), globalapi.Strong)
	require.ErrorIs(t, err, globalapi.ErrStrongResultTooLarge)
	var classified apierror.Error
	require.ErrorAs(t, err, &classified)
	require.Equal(t, apierror.Invalid, classified.Kind())
	require.Equal(t, apierror.False, classified.Retryable())
	for _, key := range []string{pendingKey("oversized"), strongResultUsageKey} {
		_, err := r.engine.Get(key)
		require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	}
}
