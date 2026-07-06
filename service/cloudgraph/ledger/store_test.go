// SPDX-License-Identifier: MPL-2.0

package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/service/cloudgraph/resource"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+t.TempDir()+"/ledger.db?_busy_timeout=5000&_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Mirror the production db.sql.sqlite pool: a single connection, so any
	// query nested inside an open transaction self-deadlocks in tests too.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := NewStore(db, nil)
	require.NoError(t, store.Migrate(context.Background()))
	return store
}

func seedOperation(t *testing.T, store *Store, deployID, resourceID string) *resource.Operation {
	t.Helper()
	ctx := context.Background()

	created, err := store.CreateDeploy(ctx, resource.DeployInput{
		DeployID: deployID,
		Scope:    "test",
		Specs: []resource.Spec{{
			ResourceID:   resourceID,
			Type:         "container",
			ProviderType: "docker",
			Desired:      json.RawMessage(`{"image":"nginx"}`),
		}},
	})
	require.NoError(t, err)
	require.True(t, created)

	opID := deployID + "/" + resourceID
	err = store.SavePlan(ctx, &PlanRecord{
		ID:         "plan/" + deployID,
		DeployID:   deployID,
		Operations: json.RawMessage(`[]`),
		Edges:      json.RawMessage(`[]`),
		Waves:      json.RawMessage(`[]`),
		SpecHash:   "hash",
	}, []resource.Operation{{
		ID:         opID,
		DeployID:   deployID,
		ResourceID: resourceID,
		Action:     resource.ActionCreate,
	}}, nil)
	require.NoError(t, err)

	op, err := store.GetOperation(ctx, opID)
	require.NoError(t, err)
	return op
}

func TestMigrateIdempotent(t *testing.T) {
	store := newTestStore(t)
	require.NoError(t, store.Migrate(context.Background()))
}

func TestCreateDeployIdempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	in := resource.DeployInput{
		DeployID: "d1",
		Scope:    "test",
		Specs: []resource.Spec{{
			ResourceID:   "test/app",
			Type:         "container",
			ProviderType: "docker",
		}},
	}

	created, err := store.CreateDeploy(ctx, in)
	require.NoError(t, err)
	require.True(t, created)

	created, err = store.CreateDeploy(ctx, in)
	require.NoError(t, err)
	require.False(t, created)

	rec, err := store.GetDeploy(ctx, "d1")
	require.NoError(t, err)
	require.Equal(t, resource.DeployPlanning, rec.Status)

	specs, err := store.LoadSpecs(ctx, "d1")
	require.NoError(t, err)
	require.Len(t, specs, 1)
}

func TestBumpIncarnationCAS(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	op := seedOperation(t, store, "d1", "test/app")

	ok, err := store.BumpIncarnation(ctx, op.ID, 1)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = store.BumpIncarnation(ctx, op.ID, 1)
	require.NoError(t, err)
	require.False(t, ok, "equal incarnation must lose the CAS")

	ok, err = store.BumpIncarnation(ctx, op.ID, 2)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = store.BumpIncarnation(ctx, op.ID, 1)
	require.NoError(t, err)
	require.False(t, ok, "older incarnation must lose the CAS")

	cur, err := store.GetOperation(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, resource.OpDispatched, cur.Status)
	require.EqualValues(t, 2, cur.Incarnation)
	require.EqualValues(t, 0, cur.LastSignal)
}

func TestBumpIncarnationResourceBusyAcrossDeploys(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	first := seedOperation(t, store, "d1", "test/app")
	second := seedOperation(t, store, "d2", "test/app")

	ok, err := store.BumpIncarnation(ctx, first.ID, 1)
	require.NoError(t, err)
	require.True(t, ok)

	_, err = store.BumpIncarnation(ctx, second.ID, 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "another operation is active", "I7 violation must surface as resource-busy")

	done, err := store.Terminalize(ctx, first.ID, 1,
		[]resource.OperationStatus{resource.OpDispatched}, resource.OpFailed, "boom")
	require.NoError(t, err)
	require.True(t, done)

	ok, err = store.BumpIncarnation(ctx, second.ID, 1)
	require.NoError(t, err)
	require.True(t, ok, "dispatch must proceed once the resource is free")
}

func TestAppendSignalContiguity(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	op := seedOperation(t, store, "d1", "test/app")

	ok, err := store.BumpIncarnation(ctx, op.ID, 1)
	require.NoError(t, err)
	require.True(t, ok)

	accepted, err := store.AppendSignal(ctx, resource.Signal{
		OperationID: op.ID, Incarnation: 1, Seq: 2,
		Kind: resource.SignalPhase, Phase: resource.PhaseAllocateStarted,
	})
	require.NoError(t, err)
	require.False(t, accepted, "seq gap must be rejected")

	accepted, err = store.AppendSignal(ctx, resource.Signal{
		OperationID: op.ID, Incarnation: 1, Seq: 1,
		Kind: resource.SignalPhase, Phase: resource.PhaseAllocateStarted,
	})
	require.NoError(t, err)
	require.True(t, accepted)

	accepted, err = store.AppendSignal(ctx, resource.Signal{
		OperationID: op.ID, Incarnation: 1, Seq: 1,
		Kind: resource.SignalPhase, Phase: resource.PhaseAllocateCommitted,
	})
	require.NoError(t, err)
	require.False(t, accepted, "duplicate seq must be rejected")

	accepted, err = store.AppendSignal(ctx, resource.Signal{
		OperationID: op.ID, Incarnation: 2, Seq: 2,
		Kind: resource.SignalPhase, Phase: resource.PhaseAllocateCommitted,
	})
	require.NoError(t, err)
	require.False(t, accepted, "stale incarnation must be rejected")

	signals, err := store.ListSignals(ctx, op.ID, 1)
	require.NoError(t, err)
	require.Len(t, signals, 1)
	require.Equal(t, resource.PhaseAllocateStarted, signals[0].Phase)
}

func TestAppendSignalConcurrentWriters(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	op := seedOperation(t, store, "d1", "test/app")

	ok, err := store.BumpIncarnation(ctx, op.ID, 1)
	require.NoError(t, err)
	require.True(t, ok)

	const writers = 8
	const attempts = 32

	var wg sync.WaitGroup
	acceptedCount := make([]int, writers)
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for seq := int64(1); seq <= attempts; seq++ {
				accepted, err := store.AppendSignal(ctx, resource.Signal{
					OperationID: op.ID, Incarnation: 1, Seq: seq,
					Kind:    resource.SignalCheckpoint,
					Payload: json.RawMessage(fmt.Sprintf(`{"writer":%d}`, w)),
				})
				require.NoError(t, err)
				if accepted {
					acceptedCount[w]++
				}
			}
		}()
	}
	wg.Wait()

	total := 0
	for _, c := range acceptedCount {
		total += c
	}
	require.Equal(t, attempts, total, "each seq must be accepted exactly once across writers")

	signals, err := store.ListSignals(ctx, op.ID, 1)
	require.NoError(t, err)
	require.Len(t, signals, attempts)
	for i, sig := range signals {
		require.EqualValues(t, i+1, sig.Seq, "accepted signals must be contiguous")
	}
}

func TestCommitOperation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	op := seedOperation(t, store, "d1", "test/app")

	ok, err := store.BumpIncarnation(ctx, op.ID, 1)
	require.NoError(t, err)
	require.True(t, ok)
	op.Incarnation = 1

	spec, err := store.GetSpec(ctx, "d1", "test/app")
	require.NoError(t, err)

	outputs := json.RawMessage(`{"container_id":"abc"}`)
	require.NoError(t, store.CommitOperation(ctx, op, spec, "test", outputs, nil))

	inst, err := store.GetInstance(ctx, "test/app")
	require.NoError(t, err)
	require.Equal(t, "active", inst.Lifecycle)
	require.EqualValues(t, 1, inst.Generation)
	require.JSONEq(t, string(outputs), string(inst.Outputs))

	err = store.CommitOperation(ctx, op, spec, "test", outputs, nil)
	require.Error(t, err, "double commit must be fenced")
}

func TestTerminalizeGuards(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	op := seedOperation(t, store, "d1", "test/app")

	ok, err := store.BumpIncarnation(ctx, op.ID, 1)
	require.NoError(t, err)
	require.True(t, ok)

	done, err := store.Terminalize(ctx, op.ID, 1,
		[]resource.OperationStatus{resource.OpPlanned}, resource.OpFailed, "boom")
	require.NoError(t, err)
	require.False(t, done, "guard set mismatch must not terminalize")

	done, err = store.Terminalize(ctx, op.ID, 2,
		[]resource.OperationStatus{resource.OpDispatched}, resource.OpFailed, "boom")
	require.NoError(t, err)
	require.False(t, done, "incarnation mismatch must not terminalize")

	done, err = store.Terminalize(ctx, op.ID, 1,
		[]resource.OperationStatus{resource.OpDispatched, resource.OpInProgress}, resource.OpFailed, "boom")
	require.NoError(t, err)
	require.True(t, done)

	cur, err := store.GetOperation(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, resource.OpFailed, cur.Status)
	require.Equal(t, "boom", cur.Error)
}

func TestFinalizeDeploy(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedOperation(t, store, "d1", "test/app")
	seedOperation(t, store, "d2", "test/other")

	seq1, err := store.FinalizeDeploy(ctx, "d1", resource.DeploySucceeded, "")
	require.NoError(t, err)
	require.EqualValues(t, 1, seq1)

	again, err := store.FinalizeDeploy(ctx, "d1", resource.DeploySucceeded, "")
	require.NoError(t, err)
	require.Equal(t, seq1, again, "repeat finalize must return the persisted seq")

	seq2, err := store.FinalizeDeploy(ctx, "d2", resource.DeployFailed, "boom")
	require.NoError(t, err)
	require.Greater(t, seq2, seq1, "finalized_seq must be strictly increasing")

	_, err = store.FinalizeDeploy(ctx, "d1", resource.DeployPlanning, "")
	require.Error(t, err, "non-terminal finalize must be rejected")
}
