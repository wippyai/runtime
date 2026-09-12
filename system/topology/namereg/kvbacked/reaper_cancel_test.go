// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// Models cancellation arriving while the authoritative read is outstanding.
// The read may finish, but cleanup must not start its next mutation afterward.
type cancelDuringCleanupRead struct {
	kvapi.Engine
	cancel context.CancelFunc
	key    string
}

func (e *cancelDuringCleanupRead) Get(key string) (kvapi.Entry, error) {
	if key == e.key {
		e.cancel()
	}
	return e.Engine.Get(key)
}

func TestCleanupCancellationDuringReadPreventsDelete(t *testing.T) {
	for _, path := range []string{"batch", "snapshot", "indexed"} {
		t.Run(path, func(t *testing.T) {
			_, engine := newParticipantTestInventory(t, 4)
			service := NewService(engine, "local", nil, nil)
			owner := mkPID("remote", "owner")
			_, err := service.Register(context.Background(), "claim", owner)
			require.NoError(t, err)
			if path != "indexed" {
				snapshot := cleanupFixtureSnapshot(t, engine)
				service.cleanupSnapshot = func(context.Context) (*participantSnapshot, error) { return snapshot, nil }
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			service.engine = &cancelDuringCleanupRead{Engine: engine, cancel: cancel, key: activeKey("claim")}
			if path == "batch" {
				err = service.reapOwners(ctx, []pid.PID{owner})[owner.String()]
			} else {
				err = service.reapBindingsContext(ctx, pidIndexBase(owner), false)
			}
			require.ErrorIs(t, err, context.Canceled)
			for _, key := range []string{activeKey("claim"), pidIndexKey(owner, "claim"), nodeIndexKey(owner, "claim")} {
				_, err := engine.Get(key)
				require.NoError(t, err, "canceled cleanup must retain %s for a later retry", key)
			}
		})
	}
}

// This backend models an authoritative forwarded read with its own cancellable
// operation. Falling back to GetViaLeader would lose that operation lifetime.
type contextualCleanupAuthority struct {
	kvapi.Engine
	cancel context.CancelFunc
}

func (e *contextualCleanupAuthority) GetViaLeader(string) (kvapi.Entry, error) {
	return kvapi.Entry{}, errParticipantUnavailable
}
func (e *contextualCleanupAuthority) GetViaLeaderContext(ctx context.Context, _ string) (kvapi.Entry, error) {
	e.cancel()
	<-ctx.Done()
	return kvapi.Entry{}, ctx.Err()
}
func TestCleanupUsesCancellableAuthorityRead(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	original := NewService(engine, "local", nil, nil)
	owner := mkPID("remote", "owner")
	_, err := original.Register(context.Background(), "claim", owner)
	require.NoError(t, err)
	snapshot := cleanupFixtureSnapshot(t, engine)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := NewService(&contextualCleanupAuthority{Engine: engine, cancel: cancel}, "local", nil, nil)
	service.cleanupSnapshot = func(context.Context) (*participantSnapshot, error) { return snapshot, nil }
	err = service.reapOwners(ctx, []pid.PID{owner})[owner.String()]
	require.ErrorIs(t, err, context.Canceled, "cleanup must pass its lifetime into the authority read")
	_, err = engine.Get(activeKey("claim"))
	require.NoError(t, err)
}

type contextualCleanupTransaction struct {
	kvapi.Engine
	cancel context.CancelFunc
}

func (e *contextualCleanupTransaction) Txn([]kvapi.TxnOp) (bool, error) {
	return false, errParticipantUnavailable
}
func (e *contextualCleanupTransaction) TxnContext(ctx context.Context, _ []kvapi.TxnOp) (bool, error) {
	e.cancel()
	<-ctx.Done()
	return false, ctx.Err()
}
func TestCleanupUsesCancellableTransaction(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	original := NewService(engine, "local", nil, nil)
	owner := mkPID("remote", "owner")
	_, err := original.Register(context.Background(), "claim", owner)
	require.NoError(t, err)
	snapshot := cleanupFixtureSnapshot(t, engine)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := NewService(&contextualCleanupTransaction{Engine: engine, cancel: cancel}, "local", nil, nil)
	service.cleanupSnapshot = func(context.Context) (*participantSnapshot, error) { return snapshot, nil }
	err = service.reapOwners(ctx, []pid.PID{owner})[owner.String()]
	require.ErrorIs(t, err, context.Canceled)
	_, err = engine.Get(activeKey("claim"))
	require.NoError(t, err)
}
