// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	systemkv "github.com/wippyai/runtime/system/kv"
)

type failingParticipantTransactions struct {
	*systemkv.Service
	fail     atomic.Bool
	failRead atomic.Bool
}

func (e *failingParticipantTransactions) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	if e.failRead.Load() {
		return nil, 0, errors.New("transient replica read")
	}
	return e.Service.ReadLocalSnapshot(keys)
}

func (e *failingParticipantTransactions) Txn(ops []kvapi.TxnOp) (bool, error) {
	if e.fail.Load() {
		return false, errors.New("transient authority loss")
	}
	return e.Service.Txn(ops)
}
func TestParticipantRecoveryReopensAfterFailedVoteWrite(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	wrapped := &failingParticipantTransactions{Service: engine}
	service := NewService(wrapped, "member", nil, nil)
	service.ConfigureStrong(StrongDeps{Incarnation: "one"})
	require.NoError(t, service.ConfigureParticipation(4))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint, err := NewParticipantEndpoint(ctx, service, &participantTestMesh{}, func(context.Context) (pid.NodeID, error) { return "member", nil }, ParticipantEndpointConfig{MaxEntries: 8, MaxValueBytes: 8192, MaxWireBytes: 16384, MaxConcurrentRequests: 1, RequestTimeout: time.Second, RefreshInterval: 10 * time.Millisecond})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, endpoint.Stop(context.Background())) })
	require.NoError(t, endpoint.Start(ctx))
	owner := mkPID("member", "owner")
	ops, err := service.strong.participants.reservationOps(ctx, pendingHeader{AttemptID: "recovery-attempt", Name: "recover", PID: owner.String(), DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano()})
	require.NoError(t, err)
	// Reserve outcome evidence in the same injected authority transaction.
	header, err := decodePending(ops[len(ops)-1].Value)
	require.NoError(t, err)
	capacity, initial, err := planStrongResultCapacity(header, service.strong.resultPolicy.RecordBytes)
	require.NoError(t, err)
	results, err := reserveStrongResultOps(engine.Get, strongResultLimits{Entries: service.strong.resultPolicy.Entries, Bytes: service.strong.resultPolicy.Bytes}, header.AttemptID, capacity, initial)
	require.NoError(t, err)
	ops = append(ops, results...)
	wrapped.fail.Store(true)
	committed, err := engine.Txn(ops)
	require.NoError(t, err)
	require.True(t, committed)
	require.Eventually(t, func() bool { return !service.NameReady() }, time.Second, time.Millisecond)
	held, ok := service.IsStrongReserved("recover")
	require.True(t, ok)
	require.True(t, held.Equal(owner))
	wrapped.fail.Store(false)
	require.Eventually(t, func() bool {
		_, err := engine.Get(activeKey("recover"))
		return err == nil && service.NameReady()
	}, 2*time.Second, time.Millisecond)
	// No new KV event is produced here. A timer's read failure must wake
	// recovery independently of watch delivery.
	wrapped.failRead.Store(true)
	service.strong.armTimer("timer-read", time.Now().UnixNano())
	require.Eventually(t, func() bool { return !service.NameReady() }, time.Second, time.Millisecond)
	wrapped.failRead.Store(false)
	require.Eventually(t, service.NameReady, time.Second, time.Millisecond)
	require.NoError(t, endpoint.Stop(ctx))
	require.False(t, service.NameReady())
}

func TestParticipantRecoveryCannotReopenOverNewerFailure(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	service := NewService(engine, "member", nil, nil)
	service.ConfigureStrong(StrongDeps{Incarnation: "one"})
	require.NoError(t, service.ConfigureParticipation(4))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint, err := NewParticipantEndpoint(ctx, service, &participantTestMesh{}, func(context.Context) (pid.NodeID, error) { return "member", nil }, ParticipantEndpointConfig{MaxEntries: 4, MaxValueBytes: 4096, MaxWireBytes: 8192, MaxConcurrentRequests: 1, RequestTimeout: time.Second, RefreshInterval: time.Millisecond})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, endpoint.Stop(context.Background())) })
	firstEntered, secondEntered := make(chan struct{}), make(chan struct{})
	firstRelease, secondRelease := make(chan struct{}), make(chan struct{})
	original := service.strong.recovery.refresh
	var calls atomic.Int32
	service.strong.recovery.refresh = func(ctx context.Context) error {
		if err := original(ctx); err != nil {
			return err
		}
		var release chan struct{}
		switch calls.Add(1) {
		case 1:
			close(firstEntered)
			release = firstRelease
		case 2:
			close(secondEntered)
			release = secondRelease
		default:
			return nil
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	require.NoError(t, endpoint.Start(ctx))
	service.invalidateParticipant()
	select {
	case <-firstEntered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// This failure happens after capture completed, before that recovery can
	// publish readiness. An older refresh cannot erase this new invalidation.
	service.invalidateParticipant()
	close(firstRelease)
	select {
	case <-secondEntered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.False(t, service.NameReady())
	close(secondRelease)
	require.Eventually(t, service.NameReady, time.Second, time.Millisecond)
	require.Equal(t, int32(2), calls.Load())
}
