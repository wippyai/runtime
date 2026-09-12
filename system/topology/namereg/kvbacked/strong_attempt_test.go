// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"testing"
	"time"
)

// Creation and observation are separate operations. A reconciler can finish
// the committed reservation while its creator is still returning from Txn.
type attemptCreateObserver struct {
	kvapi.Engine
	after func()
}

func (e *attemptCreateObserver) Txn(ops []kvapi.TxnOp) (bool, error) {
	committed, err := e.Engine.Txn(ops)
	if committed && err == nil && e.after != nil {
		for _, op := range ops {
			if op.Kind == kvapi.TxnPut && op.Key == pendingKey("claim") {
				after := e.after
				e.after = nil
				after()
				break
			}
		}
	}
	return committed, err
}

func TestStrongCompletionBeforeWaiterAttachment(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	owner := mkPID("node-1", "owner")
	engine := r.engine
	promoted := false
	r.engine = &attemptCreateObserver{Engine: engine, after: func() {
		entry, err := engine.Get(pendingKey("claim"))
		require.NoError(t, err)
		header, err := decodePending(entry.Value)
		require.NoError(t, err)
		require.NoError(t, r.strong.attestHeader("claim", entry.Epoch, owner, header))
		r.strong.leaderPromote("claim", entry.Epoch, entry.Version, header)
		active, err := engine.Get(activeKey("claim"))
		require.NoError(t, err, "fixture must commit promotion before creator resumes")
		value, err := decodeActive(active.Value)
		require.NoError(t, err)
		require.Equal(t, owner.String(), value.PID)
		promoted = true
	}}
	out, err := r.RegisterScope(context.Background(), "claim", owner, globalapi.Strong)
	require.True(t, promoted)
	require.NoError(t, err, "a committed successful claim must remain observable by its creator")
	require.Equal(t, globalapi.RegisterStateActive, out.State)
	require.Equal(t, owner.String(), out.PID.String())
}

func TestStrongCreationCannotAdoptReplacement(t *testing.T) {
	for _, state := range []string{"active", "pending"} {
		t.Run(state, func(t *testing.T) {
			r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
			owner := mkPID("node-1", "same-owner")
			engine := r.engine
			r.engine = &attemptCreateObserver{Engine: engine, after: func() {
				entry, err := engine.Get(pendingKey("claim"))
				require.NoError(t, err)
				old, err := decodePending(entry.Value)
				require.NoError(t, err)
				require.NotEmpty(t, old.AttemptID)
				key := activeKey("claim")
				var body []byte
				if state == "active" {
					body, err = encode(activeValue{AttemptID: "replacement", PID: owner.String(), Name: "claim", Strong: true})
				} else {
					key = pendingKey("claim")
					old.AttemptID = "replacement"
					body, err = encode(old)
				}
				require.NoError(t, err)
				committed, err := engine.Txn([]kvapi.TxnOp{
					{Kind: kvapi.TxnDelete, Cond: kvapi.CondVersion, Key: pendingKey("claim"), Expect: entry.Version},
					{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: key, Value: body},
				})
				require.NoError(t, err)
				require.True(t, committed)
			}}
			out, err := r.RegisterScope(context.Background(), "claim", owner, globalapi.Strong)
			require.ErrorIs(t, err, globalapi.ErrNotAvailable)
			require.NotEqual(t, globalapi.RegisterStateActive, out.State)
		})
	}
}

func TestStrongCreationPreservesUndecodableReplacement(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	owner := mkPID("node-1", "owner")
	engine := r.engine
	malformed := []byte{0xc1}
	r.engine = &attemptCreateObserver{Engine: engine, after: func() {
		_, err := engine.Set(pendingKey("claim"), malformed)
		require.NoError(t, err)
	}}
	_, err := r.RegisterScope(context.Background(), "claim", owner, globalapi.Strong)
	require.Error(t, err)
	entry, err := engine.Get(pendingKey("claim"))
	require.NoError(t, err, "unrecognized replacement is not this caller's claim to delete")
	require.Equal(t, malformed, entry.Value)
}

// Both local replicas and forwarding snapshots must bind successful delivery
// to the attempt, not just a name/PID which a later registration can reuse.
func TestStrongActiveDeliveryBindsAttempt(t *testing.T) {
	for _, path := range []string{"replica", "forwarding"} {
		t.Run(path, func(t *testing.T) {
			inventory, authority := newParticipantTestInventory(t, 4)
			_, local := newParticipantTestInventory(t, 4)
			service := NewService(authority, "client", nil, nil)
			if path == "forwarding" {
				service = NewService(local, "client", nil, nil)
			}
			service.ConfigureStrong(StrongDeps{Incarnation: "one", IsLeader: func() bool { return false }})
			limits := participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}
			_, err := service.bootstrapParticipant(context.Background(), inventory, authority, limits)
			require.NoError(t, err)
			owner := mkPID("client", "same-owner")
			value, err := encode(activeValue{AttemptID: "current", Name: "name", PID: owner.String(), Strong: true})
			require.NoError(t, err)
			_, err = authority.Set(activeKey("name"), value)
			require.NoError(t, err)
			old := &strongWaiter{attemptID: "old", ch: make(chan globalapi.RegisterOutcome, 1)}
			current := &strongWaiter{attemptID: "current", ch: make(chan globalapi.RegisterOutcome, 1)}
			unknown := &strongWaiter{ch: make(chan globalapi.RegisterOutcome, 1)}
			for _, w := range []*strongWaiter{old, current, unknown} {
				service.strong.addWaiter("name", w)
				defer service.strong.removeWaiter("name", w)
			}
			if path == "forwarding" {
				_, err = service.refreshParticipant(context.Background(), inventory, authority, limits)
			} else {
				err = service.strong.reconcile("name")
			}
			require.NoError(t, err)
			select {
			case out := <-current.ch:
				require.Equal(t, globalapi.RegisterStateActive, out.State)
				require.True(t, out.PID.Equal(owner))
			default:
				t.Fatal("matching attempt received no active result")
			}
			for _, w := range []*strongWaiter{old, unknown} {
				select {
				case out := <-w.ch:
					t.Fatalf("unrelated attempt %q adopted active result: %+v", w.attemptID, out)
				default:
				}
			}
		})
	}
}

// A terminal active observation can arrive after Get captured the pending
// entry, but before Get returns it to the registration goroutine.
type attemptPendingReadObserver struct {
	kvapi.Engine
	after func(kvapi.Entry)
}

func (e *attemptPendingReadObserver) Get(key string) (kvapi.Entry, error) {
	entry, err := e.Engine.Get(key)
	if key == pendingKey("claim") && err == nil && e.after != nil {
		after := e.after
		e.after = nil
		after(entry)
	}
	return entry, err
}
func TestStrongCompletionDuringPendingRead(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	owner := mkPID("node-1", "owner")
	engine := r.engine
	completed := false
	r.engine = &attemptPendingReadObserver{Engine: engine, after: func(entry kvapi.Entry) {
		header, err := decodePending(entry.Value)
		require.NoError(t, err)
		require.NoError(t, r.strong.attestHeader("claim", entry.Epoch, owner, header))
		r.strong.leaderPromote("claim", entry.Epoch, entry.Version, header)
		_, err = engine.Get(activeKey("claim"))
		require.NoError(t, err)
		completed = true
	}}
	// The deadline bounds a broken implementation only; the event interleaving
	// above is synchronous and deterministic.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := r.RegisterScope(ctx, "claim", owner, globalapi.Strong)
	require.True(t, completed)
	require.NoError(t, err)
	require.Equal(t, globalapi.RegisterStateActive, out.State)
	require.True(t, out.PID.Equal(owner))
}

func TestStrongUnboundWaiterIgnoresPriorAbsence(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	w := &strongWaiter{attemptID: "new", awaitingEpoch: true, ch: make(chan globalapi.RegisterOutcome, 1)}
	r.strong.addWaiter("claim", w)
	defer r.strong.removeWaiter("claim", w)
	before := r.strong.observeTerminal("claim")
	r.strong.mu.Lock()
	w.epoch = 1
	w.awaitingEpoch = false
	r.strong.mu.Unlock()
	// Even a later-index absence observed before epoch binding is not evidence
	// about the new attempt. Test index 2 so epoch comparison cannot mask this.
	r.strong.onTerminal("claim", before, 2)
	select {
	case out := <-w.ch:
		t.Fatalf("pre-binding observation completed new attempt: %+v", out)
	default:
	}
	r.strong.onTerminal("claim", r.strong.observeTerminal("claim"), 2)
	select {
	case out := <-w.ch:
		require.Equal(t, uint64(1), out.Epoch)
	default:
		t.Fatal("bound waiter did not receive subsequent observation")
	}
}

func TestStrongCapturedPromotionSurvivesActiveRemoval(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	owner := mkPID("node-1", "owner")
	engine := r.engine
	r.engine = &attemptCreateObserver{Engine: engine, after: func() {
		entry, err := engine.Get(pendingKey("claim"))
		require.NoError(t, err)
		hdr, err := decodePending(entry.Value)
		require.NoError(t, err)
		require.NoError(t, r.strong.attestHeader("claim", entry.Epoch, owner, hdr))
		r.strong.leaderPromote("claim", entry.Epoch, entry.Version, hdr)
		require.NoError(t, engine.Delete(activeKey("claim")))
	}}
	out, err := r.RegisterScope(context.Background(), "claim", owner, globalapi.Strong)
	require.NoError(t, err)
	require.Equal(t, globalapi.RegisterStateActive, out.State)
	r.strong.mu.Lock()
	remaining := len(r.strong.waiters)
	r.strong.mu.Unlock()
	require.Zero(t, remaining, "registration must release its waiter")
}

// Preserve the production engine's optional snapshot surface while intercepting
// a single operation; reconciliation must still execute its real read path.
func (e *attemptCreateObserver) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	return e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
}
func (e *attemptPendingReadObserver) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	return e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
}
