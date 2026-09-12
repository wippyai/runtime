// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

type retainedParticipantSnapshot struct {
	revision uint64
	entries  []kvapi.Entry
}

func (s retainedParticipantSnapshot) ScanAtIndex(_ string, fn func(kvapi.Entry) bool) (uint64, error) {
	for _, entry := range s.entries {
		if !fn(entry) {
			break
		}
	}
	return s.revision, nil
}

func TestParticipantRefreshRetiresOnlyObservedGeneration(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "terminal", true: "replacement-during-capture"}[replacement], func(t *testing.T) {
			inventory, authority := newParticipantTestInventory(t, 4)
			_, clientEngine := newParticipantTestInventory(t, 4)
			client := NewService(clientEngine, "client", nil, nil)
			client.ConfigureStrong(StrongDeps{Incarnation: "one", IsLeader: func() bool { return false }})
			ctx := context.Background()
			old := mkPID("owner", "old")
			next := old.Precomputed()
			value, err := encode(activeValue{Name: "name", PID: old.String(), Strong: true})
			require.NoError(t, err)
			_, err = authority.Set(activeKey("name"), value)
			require.NoError(t, err)
			limits := participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}
			_, err = client.bootstrapParticipant(ctx, inventory, authority, limits)
			require.NoError(t, err)
			oldEpoch := client.strong.exclusions["name"].epoch
			require.NoError(t, authority.Delete(activeKey("name")))
			source := enrollmentDuringSnapshot{participantSnapshotSource: authority, enroll: func() {
				if replacement {
					// The captured authority view predates this new generation. Even an
					// already-observed claim refreshed at the same epoch must survive.
					learned, err := client.strong.learnExclusion("name", next, oldEpoch, exclusionPending)
					require.NoError(t, err)
					require.True(t, learned)
				}
			}}
			_, err = client.refreshParticipant(ctx, inventory, source, limits)
			require.NoError(t, err)
			got, held := client.IsStrongReserved("name")
			require.Equal(t, replacement, held)
			if replacement {
				require.True(t, got.Equal(next))
			}
		})
	}
}

func TestParticipantRefreshRejectsOldCompleteSnapshot(t *testing.T) {
	inventory, authority := newParticipantTestInventory(t, 4)
	_, clientEngine := newParticipantTestInventory(t, 4)
	client := NewService(clientEngine, "client", nil, nil)
	client.ConfigureStrong(StrongDeps{Incarnation: "one", IsLeader: func() bool { return false }})
	ctx := context.Background()
	limits := participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}
	_, err := client.bootstrapParticipant(ctx, inventory, authority, limits)
	require.NoError(t, err)
	var stale retainedParticipantSnapshot
	stale.revision, err = authority.ScanAtIndex(registryPrefix, func(entry kvapi.Entry) bool { stale.entries = append(stale.entries, entry); return true })
	require.NoError(t, err)
	owner := mkPID("owner", "owner")
	value, err := encode(activeValue{Name: "name", PID: owner.String(), Strong: true})
	require.NoError(t, err)
	_, err = authority.Set(activeKey("name"), value)
	require.NoError(t, err)
	_, err = client.refreshParticipant(ctx, inventory, authority, limits)
	require.NoError(t, err)
	client.ready.Store(true)
	_, err = client.refreshParticipant(ctx, inventory, stale, limits)
	require.ErrorContains(t, err, "revision regressed")
	require.False(t, client.NameReady())
	got, held := client.IsStrongReserved("name")
	require.True(t, held)
	require.True(t, got.Equal(owner))
}

func TestParticipantClientCannotRetireFromEmptyLocalReplica(t *testing.T) {
	_, engine := newParticipantTestInventory(t, 4)
	client := NewService(engine, "client", nil, nil)
	client.SetNonMember(func() bool { return true })
	client.ConfigureStrong(StrongDeps{Incarnation: "one", IsLeader: func() bool { return false }})
	owner := mkPID("owner", "owner")
	learned, err := client.strong.learnExclusion("name", owner, 1, exclusionActive)
	require.NoError(t, err)
	require.True(t, learned)
	require.ErrorContains(t, client.StartReconciler(context.Background()), "requires an authority feed")
	require.Nil(t, client.reconciler.Load(), "unsupported startup must not create watch ownership")
	client.ready.Store(true)
	require.ErrorContains(t, client.strong.reconcile("name"), "requires an authority feed")
	require.False(t, client.NameReady())
	got, held := client.IsStrongReserved("name")
	require.True(t, held)
	require.True(t, got.Equal(owner))
}

func TestParticipantRefreshAttestsWithoutWithdrawingLiveConflict(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "ack", true: "reject"}[conflict], func(t *testing.T) {
			inventory, authority := newParticipantTestInventory(t, 4)
			ctx := context.Background()
			require.NoError(t, inventory.enroll(ctx, "owner", "owner-incarnation"))
			_, localEngine := newParticipantTestInventory(t, 4)
			client := NewService(participantForwardingEngine{Engine: localEngine, authority: authority}, "client", nil, nil)
			client.SetNonMember(func() bool { return true })
			weak := mkPID("client", "weak")
			revoker := &countingParticipantRevoker{}
			client.ConfigureStrong(StrongDeps{
				Incarnation: "one", IsLeader: func() bool { return false }, LocalRevoker: revoker,
				LocalConflict: func(string, pid.PID) (pid.PID, bool) { return weak, conflict },
			})
			limits := participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}
			_, err := client.bootstrapParticipant(ctx, inventory, authority, limits)
			require.NoError(t, err)
			owner := mkPID("owner", "owner")
			ops, err := inventory.reservationOps(ctx, pendingHeader{Name: "name", PID: owner.String()})
			require.NoError(t, err)
			committed, err := authority.Txn(ops)
			require.NoError(t, err)
			require.True(t, committed)
			pending, err := authority.Get(pendingKey("name"))
			require.NoError(t, err)
			header, err := decodePending(pending.Value)
			require.NoError(t, err)
			_, err = client.refreshParticipant(ctx, inventory, authority, limits)
			require.NoError(t, err)
			require.Zero(t, revoker.calls, "new live reservations must attest, never withdraw local ownership")
			expected, absent := header.ackKey("name", pending.Epoch, "client"), header.rejectKey("name", pending.Epoch, "client")
			if conflict {
				expected, absent = absent, expected
			}
			_, err = authority.Get(expected)
			require.NoError(t, err)
			_, err = authority.Get(absent)
			require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
			got, held := client.IsStrongReserved("name")
			require.Equal(t, !conflict, held)
			if held {
				require.True(t, got.Equal(owner))
			}
			if conflict {
				conflict = false
				_, err = client.refreshParticipant(ctx, inventory, authority, limits)
				require.NoError(t, err)
				_, err = authority.Get(header.ackKey("name", pending.Epoch, "client"))
				require.ErrorIs(t, err, kvapi.ErrKeyNotFound, "an earlier authoritative reject must not become an ACK after the local conflict disappears")
			}
		})
	}
}

type countingParticipantRevoker struct{ calls int }

func (r *countingParticipantRevoker) RevokeLocal(string, pid.PID) bool    { r.calls++; return false }
func (r *countingParticipantRevoker) RevokeEventual(string, pid.PID) bool { r.calls++; return false }

// Transport-free fixture with the production engine's client semantics:
// local reads are empty, leader reads and mutations go to the authority.
type participantForwardingEngine struct {
	kvapi.Engine
	authority kvapi.Engine
}

func (e participantForwardingEngine) GetViaLeader(key string) (kvapi.Entry, error) {
	return e.authority.Get(key)
}
func (e participantForwardingEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	return e.authority.Txn(ops)
}

// Forwarding clients have no replicated watch: an authority refresh must
// deliver the terminal observation to an outstanding Strong registration.
func TestParticipantRefreshCompletesStrongWaiter(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "canceled"}[canceled], func(t *testing.T) {
			inventory, authority := newParticipantTestInventory(t, 4)
			_, local := newParticipantTestInventory(t, 4)
			client := NewService(local, "client", nil, nil)
			client.ConfigureStrong(StrongDeps{Incarnation: "one", IsLeader: func() bool { return false }})
			limits := participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}
			_, err := client.bootstrapParticipant(context.Background(), inventory, authority, limits)
			require.NoError(t, err)
			owner := mkPID("client", "owner")
			value, err := encode(activeValue{Name: "name", PID: owner.String(), Strong: true})
			require.NoError(t, err)
			_, err = authority.Set(activeKey("name"), value)
			require.NoError(t, err)
			entry, err := authority.Get(activeKey("name"))
			require.NoError(t, err)
			waiter := &strongWaiter{epoch: entry.Epoch, ch: make(chan globalapi.RegisterOutcome, 1)}
			client.strong.addWaiter("name", waiter)
			defer client.strong.removeWaiter("name", waiter)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if canceled {
				cancel()
			}
			_, err = client.refreshParticipant(ctx, inventory, authority, limits)
			if canceled {
				require.ErrorIs(t, err, context.Canceled)
				select {
				case <-waiter.ch:
					t.Fatal("canceled refresh completed registration")
				default:
				}
				return
			}
			require.NoError(t, err)
			select {
			case out := <-waiter.ch:
				require.Equal(t, globalapi.RegisterStateActive, out.State)
				require.Equal(t, entry.Epoch, out.Epoch)
				require.True(t, out.PID.Equal(owner))
			default:
				t.Fatal("authority active snapshot did not complete forwarding waiter")
			}
		})
	}
}
