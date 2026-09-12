// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

func TestStrongAcknowledgementReusesCapacityWithoutRetentionWait(t *testing.T) {
	r := newStrongReg(t, []string{"node-1"}, time.Second, nil)
	policy := DefaultStrongResultPolicy()
	policy.Entries, policy.ReclaimBatch = 1, 1
	policy.Retention = time.Hour
	require.NoError(t, r.ConfigureStrongResults(policy))
	owner := mkPID("node-1", "owner")
	for i := 0; i < 128; i++ {
		out, err := r.RegisterScope(context.Background(), fmt.Sprintf("name-%d", i), owner, globalapi.Strong)
		require.NoError(t, err)
		require.Equal(t, globalapi.RegisterStateActive, out.State)
		usage, _, err := readStrongResultUsage(r.engine.Get)
		require.NoError(t, err)
		require.Equal(t, strongResultUsage{}, usage)
	}
}

type refuseResultACK struct{ kvapi.Engine }

func (e *refuseResultACK) Txn(ops []kvapi.TxnOp) (bool, error) {
	for _, op := range ops {
		if op.Kind == kvapi.TxnDelete && strings.HasPrefix(op.Key, strongResultPrefix) {
			return false, context.DeadlineExceeded
		}
	}
	return e.Engine.Txn(ops)
}

func TestStrongAcknowledgementFailurePreservesKnownSuccess(t *testing.T) {
	r := newStrongReg(t, []string{"node-1"}, time.Second, nil)
	r.engine = &refuseResultACK{Engine: r.engine}
	owner := mkPID("node-1", "owner")
	out, err := r.RegisterScope(context.Background(), "claim", owner, globalapi.Strong)
	require.NoError(t, err)
	require.Equal(t, globalapi.RegisterStateActive, out.State)
	usage, _, err := readStrongResultUsage(r.engine.Get)
	require.NoError(t, err)
	require.Equal(t, uint64(1), usage.Entries, "retention owns cleanup after failed acknowledgement")
	_, err = r.engine.Get(activeKey("claim"))
	require.NoError(t, err)
}

func TestStrongAcknowledgementCannotReleasePendingOrForeignAttempt(t *testing.T) {
	r := newStrongReg(t, []string{"node-1"}, time.Second, nil)
	owner := mkPID("node-1", "owner")
	header, entry := seedStrongPending(t, r, pendingHeader{Name: "claim", PID: owner.String(), RequiredNodes: []string{"node-1"}})
	require.NoError(t, r.strong.acknowledgeStrongResult(context.Background(), "claim", owner, header.AttemptID))
	_, err := r.engine.Get(strongResultKey(header.AttemptID))
	require.NoError(t, err)
	r.strong.leaderExpire("claim", entry.Epoch, entry.Version, header, "unreserve")
	require.Error(t, r.strong.acknowledgeStrongResult(context.Background(), "wrong", owner, header.AttemptID))
	require.Error(t, r.strong.acknowledgeStrongResult(context.Background(), "claim", mkPID("node-1", "foreign"), header.AttemptID))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, r.strong.acknowledgeStrongResult(ctx, "claim", owner, header.AttemptID), context.Canceled)
	_, err = r.engine.Get(strongResultKey(header.AttemptID))
	require.NoError(t, err)
	require.NoError(t, r.strong.acknowledgeStrongResult(context.Background(), "claim", owner, header.AttemptID))
	require.NoError(t, r.strong.acknowledgeStrongResult(context.Background(), "claim", owner, header.AttemptID), "duplicate ACK is harmless")
	usage, _, err := readStrongResultUsage(r.engine.Get)
	require.NoError(t, err)
	require.Equal(t, strongResultUsage{}, usage)
}

func (e *refuseResultACK) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	return e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
}

// Commit the acknowledgement, then lose only its reply. This must not replay
// the accounting mutation or turn the already observed registration into error.
type lostResultACK struct {
	kvapi.Engine
	acknowledgements int
}

func (e *lostResultACK) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	return e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
}
func (e *lostResultACK) Txn(ops []kvapi.TxnOp) (bool, error) {
	committed, err := e.Engine.Txn(ops)
	if err != nil || !committed {
		return committed, err
	}
	for _, op := range ops {
		if op.Kind == kvapi.TxnDelete && strings.HasPrefix(op.Key, strongResultPrefix) {
			e.acknowledgements++
			return false, context.DeadlineExceeded
		}
	}
	return committed, nil
}
func TestStrongAcknowledgementLostReplyDoesNotReplay(t *testing.T) {
	r := newStrongReg(t, []string{"node-1"}, time.Second, nil)
	engine := &lostResultACK{Engine: r.engine}
	r.engine = engine
	owner := mkPID("node-1", "owner")
	out, err := r.RegisterScope(context.Background(), "claim", owner, globalapi.Strong)
	require.NoError(t, err)
	require.Equal(t, globalapi.RegisterStateActive, out.State)
	require.Equal(t, 1, engine.acknowledgements)
	usage, _, err := readStrongResultUsage(engine.Get)
	require.NoError(t, err)
	require.Equal(t, strongResultUsage{}, usage)
	_, err = engine.Get(activeKey("claim"))
	require.NoError(t, err)
}

func TestStrongConcurrentAdmissionAcknowledgementAccounting(t *testing.T) {
	r := newStrongReg(t, []string{"node-1"}, 5*time.Second, nil)
	const workers, perWorker = 16, 16
	failures := make(chan error, workers*perWorker)
	var wg sync.WaitGroup
	start := make(chan struct{})
	began := time.Now()
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			owner := mkPID("node-1", fmt.Sprintf("owner-%d", worker))
			for n := 0; n < perWorker; n++ {
				out, err := r.RegisterScope(context.Background(), fmt.Sprintf("claim-%d-%d", worker, n), owner, globalapi.Strong)
				if err != nil {
					failures <- err
					continue
				}
				if out.State != globalapi.RegisterStateActive {
					failures <- fmt.Errorf("unexpected outcome: %+v", out)
				}
			}
		}(worker)
	}
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Errorf("concurrent admission: %v", err)
	}
	usage, _, err := readStrongResultUsage(r.engine.Get)
	require.NoError(t, err)
	var actual strongResultUsage
	require.NoError(t, r.engine.Scan(strongResultPrefix, func(entry kvapi.Entry) bool {
		result, err := decodeStrongOutcome(entry, r.strong.resultPolicy.RecordBytes)
		require.NoError(t, err)
		require.Equal(t, strongResultActive, result.Phase)
		var reservation strongResultReservation
		require.NoError(t, decodeInto(entry.Value, &reservation))
		actual.Entries++
		actual.Bytes += reservation.Capacity
		return true
	}))
	require.Equal(t, actual, usage, "contended best-effort ACK must retain exact quota accounting")
	t.Logf("local engine: %d registrations, %d workers, elapsed %s, retained ACK contention records %d", workers*perWorker, workers, time.Since(began), actual.Entries)
}

func TestStrongAcknowledgementRefreshesQuotaAfterDefinitiveRefusal(t *testing.T) {
	r := newStrongReg(t, []string{"node-1"}, time.Second, nil)
	owner := mkPID("node-1", "owner")
	header, entry := seedStrongPending(t, r, pendingHeader{Name: "claim", PID: owner.String(), RequiredNodes: []string{"node-1"}})
	r.strong.leaderExpire("claim", entry.Epoch, entry.Version, header, "unreserve")
	engine := &resultGCConcurrentWrite{Engine: r.engine}
	engine.before = func() {
		seedStrongPending(t, r, pendingHeader{Name: "concurrent", PID: owner.String(), RequiredNodes: []string{"node-1"}})
	}
	r.engine = engine
	require.NoError(t, r.strong.acknowledgeStrongResult(context.Background(), "claim", owner, header.AttemptID))
	_, err := engine.Get(strongResultKey(header.AttemptID))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound, "retry must refresh stale quota rather than wait for retention")
	usage, _, err := readStrongResultUsage(engine.Get)
	require.NoError(t, err)
	require.Equal(t, uint64(1), usage.Entries)
	_, err = engine.Get(pendingKey("concurrent"))
	require.NoError(t, err)
}
