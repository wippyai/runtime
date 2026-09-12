// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/api/topology"
)

func cleanupFixtureSnapshot(t *testing.T, engine kvapi.Engine) *participantSnapshot {
	t.Helper()
	snapshot := &participantSnapshot{Entries: make(map[string]kvapi.Entry)}
	require.NoError(t, engine.Scan(activePrefix, func(entry kvapi.Entry) bool { snapshot.Entries[entry.Key] = entry; return true }))
	return snapshot
}

type selectiveCleanupFailure struct {
	kvapi.Engine
	failingKey string
	calls      int
}

func (e *selectiveCleanupFailure) Txn(ops []kvapi.TxnOp) (bool, error) {
	e.calls++
	for _, op := range ops {
		if op.Key == e.failingKey {
			return false, errParticipantUnavailable
		}
	}
	return e.Engine.Txn(ops)
}

func TestCleanupBatchUsesOneSnapshotAndRetriesOnlyFailedOwner(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	s := NewService(engine, "local", nil, nil)
	a, b, untouched := mkPID("remote", "a"), mkPID("remote", "b"), mkPID("remote", "keep")
	for name, owner := range map[string]pid.PID{"a": a, "b": b, "keep": untouched} {
		_, err := s.Register(context.Background(), name, owner)
		require.NoError(t, err)
	}
	captures := 0
	s.cleanupSnapshot = func(context.Context) (*participantSnapshot, error) {
		captures++
		return cleanupFixtureSnapshot(t, engine), nil
	}
	faulty := &selectiveCleanupFailure{Engine: engine, failingKey: activeKey("a")}
	s.engine = faulty
	outcomes := s.reapOwners(context.Background(), []pid.PID{a, a, b})
	require.Len(t, outcomes, 2)
	require.Equal(t, 1, captures)
	require.Equal(t, 2, faulty.calls, "duplicate owner must not duplicate writes")
	require.ErrorIs(t, outcomes[a.String()], errParticipantUnavailable)
	require.NoError(t, outcomes[b.String()])
	_, err := engine.Get(activeKey("b"))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	_, err = engine.Get(activeKey("a"))
	require.NoError(t, err)
	_, err = engine.Get(activeKey("keep"))
	require.NoError(t, err)
	faulty.failingKey = ""
	outcomes = s.reapOwners(context.Background(), []pid.PID{a})
	require.NoError(t, outcomes[a.String()])
	require.Equal(t, 2, captures)
	require.Equal(t, 3, faulty.calls)
	_, err = engine.Get(activeKey("a"))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
}

func TestCleanupBatchRejectsIncompleteEnumerationBeforeMutation(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	s := NewService(engine, "local", nil, nil)
	owner := mkPID("remote", "owner")
	_, err := s.Register(context.Background(), "valid", owner)
	require.NoError(t, err)
	snapshot := cleanupFixtureSnapshot(t, engine)
	snapshot.Entries[activeKey("corrupt")] = kvapi.Entry{Key: activeKey("corrupt"), Value: []byte("invalid")}
	s.cleanupSnapshot = func(context.Context) (*participantSnapshot, error) { return snapshot, nil }
	counted := &selectiveCleanupFailure{Engine: engine}
	s.engine = counted
	outcomes := s.reapOwners(context.Background(), []pid.PID{owner})
	require.Error(t, outcomes[owner.String()])
	require.Zero(t, counted.calls, "no partial enumeration may become a mutation")
	_, err = engine.Get(activeKey("valid"))
	require.NoError(t, err)
}

func TestExitCleanupFailureRetainsObservationAndDeduplicatesPayloads(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	s := NewService(engine, "local", nil, nil)
	owner := mkPID("remote", "owner")
	s.monitored.Store(owner.String(), struct{}{})
	_, err := s.Register(context.Background(), "name", owner)
	require.NoError(t, err)
	captures := 0
	fail := true
	s.cleanupSnapshot = func(context.Context) (*participantSnapshot, error) {
		captures++
		if fail {
			return nil, errParticipantUnavailable
		}
		return cleanupFixtureSnapshot(t, engine), nil
	}
	event := func() *relay.Package {
		body := payload.New(&topology.ExitEvent{From: owner, Kind: topology.Exit})
		return relay.NewPackage(owner, s.self, topology.TopicEvents, body, body)
	}
	require.NoError(t, s.Send(event()))
	require.Equal(t, 1, captures)
	_, monitored := s.monitored.Load(owner.String())
	require.True(t, monitored, "failed cleanup must not forget the owner")
	fail = false
	// Explicit retry here; this test does not imply an automatic retry worker.
	require.NoError(t, s.Send(event()))
	require.Equal(t, 2, captures)
	_, monitored = s.monitored.Load(owner.String())
	require.False(t, monitored)
	_, err = engine.Get(activeKey("name"))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
}

func TestCleanupBatchDoesNotEraseReplacementOwner(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	s := NewService(engine, "local", nil, nil)
	old, next := mkPID("remote", "old"), mkPID("remote", "next")
	_, err := s.Register(context.Background(), "reused", old)
	require.NoError(t, err)
	snapshot := cleanupFixtureSnapshot(t, engine)
	// Replace after the authoritative enumeration. Each delete must still check
	// the current owner/version rather than treating snapshot data as authority
	// to unconditionally remove the live binding.
	removed, err := s.Unregister(context.Background(), "reused")
	require.NoError(t, err)
	require.True(t, removed)
	_, err = s.Register(context.Background(), "reused", next)
	require.NoError(t, err)
	s.cleanupSnapshot = func(context.Context) (*participantSnapshot, error) { return snapshot, nil }
	outcomes := s.reapOwners(context.Background(), []pid.PID{old})
	require.NoError(t, outcomes[old.String()])
	found, err := s.Lookup(context.Background(), "reused")
	require.NoError(t, err)
	require.True(t, found.Found)
	require.True(t, found.PID.Equal(next))
	_, err = engine.Get(pidIndexKey(next, "reused"))
	require.NoError(t, err)
}

type cancelAfterCleanupCommit struct {
	kvapi.Engine
	cancel       context.CancelFunc
	committedKey string
}

func (e *cancelAfterCleanupCommit) Txn(ops []kvapi.TxnOp) (bool, error) {
	committed, err := e.Engine.Txn(ops)
	if committed && err == nil {
		for _, op := range ops {
			if op.Kind == kvapi.TxnDelete && (op.Key == activeKey("a") || op.Key == activeKey("b")) {
				e.committedKey = op.Key
			}
		}
		e.cancel()
	}
	return committed, err
}

func TestCleanupBatchCancellationPreservesCompletedOwnerOutcome(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	s := NewService(engine, "local", nil, nil)
	a, b := mkPID("remote", "a"), mkPID("remote", "b")
	for name, owner := range map[string]pid.PID{"a": a, "b": b} {
		_, err := s.Register(context.Background(), name, owner)
		require.NoError(t, err)
	}
	s.cleanupSnapshot = func(context.Context) (*participantSnapshot, error) { return cleanupFixtureSnapshot(t, engine), nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	canceling := &cancelAfterCleanupCommit{Engine: engine, cancel: cancel}
	s.engine = canceling
	outcomes := s.reapOwners(ctx, []pid.PID{a, b})
	completed, pending, pendingName := a, b, "b"
	if canceling.committedKey == activeKey("b") {
		completed, pending, pendingName = b, a, "a"
	}
	require.NotEmpty(t, canceling.committedKey)
	require.NoError(t, outcomes[completed.String()], "a committed owner does not become unfinished because a later operation is canceled")
	require.ErrorIs(t, outcomes[pending.String()], context.Canceled)
	_, err := engine.Get(canceling.committedKey)
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	_, err = engine.Get(activeKey(pendingName))
	require.NoError(t, err)
}
