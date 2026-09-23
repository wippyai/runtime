// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	systemkv "github.com/wippyai/runtime/system/kv"
)

// A roster update between the submitter's local read and the pending write
// must abort that write. The retry then includes the newly enrolled node,
// despite any stale gossip view the submitter might have held.
type beforePendingTxnEngine struct {
	kvapi.Engine
	before func()
	once   sync.Once
}

type flakyActivationEngine struct {
	kvapi.Engine
	remaining atomic.Int32
}

type blockedActivationEngine struct {
	kvapi.Engine
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
	calls    atomic.Int32
}

func (e *blockedActivationEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	for _, op := range ops {
		if op.Kind == kvapi.TxnPut && op.Key == participantsKey {
			if e.calls.Add(1) == 1 {
				select {
				case e.started <- struct{}{}:
				default:
				}
				<-e.release
				defer close(e.finished)
			}
			break
		}
	}
	return e.Engine.Txn(ops)
}

func (e *flakyActivationEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	for _, op := range ops {
		if op.Kind == kvapi.TxnPut && op.Key == participantsKey && e.remaining.Add(-1) >= 0 {
			return false, errors.New("leader temporarily unavailable")
		}
	}
	return e.Engine.Txn(ops)
}

func (e *beforePendingTxnEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	for _, op := range ops {
		if op.Kind == kvapi.TxnPut && strings.HasPrefix(op.Key, pendingPrefix) {
			e.once.Do(e.before)
			break
		}
	}
	return e.Engine.Txn(ops)
}

func TestStrongPendingRetriesAgainstCommittedParticipantRoster(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	startStrongReconciler(t, r)
	base := r.engine.(*systemkv.Service)
	r.engine = &beforePendingTxnEngine{Engine: base, before: func() {
		addTestParticipant(t, base, "node-2")
	}}
	owner := mkPID("node-1", "owner")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.RegisterScope(ctx, "claim", owner, globalapi.Strong)
		done <- err
	}()
	if !eventually(t, time.Second, func() bool {
		_, err := base.Get(pendingKey("claim"))
		return err == nil
	}) {
		t.Fatal("retry did not publish the pending claim")
	}
	entry, err := base.Get(pendingKey("claim"))
	if err != nil {
		t.Fatal(err)
	}
	hdr, err := decodePending(entry.Value)
	if err != nil || !contains(hdr.RequiredNodes, "node-2") {
		t.Fatalf("committed roster addition was omitted: header=%+v err=%v", hdr, err)
	}
	if _, err := base.Get(activeKey("claim")); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("claim promoted without node-2 ACK: %v", err)
	}
	cancel()
	<-done
}

func TestPendingBeforeEnrollmentIsSeededBeforeAdmission(t *testing.T) {
	base := systemkv.NewService("pre-enrollment", nil)
	if _, err := base.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Stop(context.Background()) })
	roster, err := encode(participantsValue{Nodes: []participantEntry{{Node: "node-1", Activation: "old"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.Set(participantsKey, roster); err != nil {
		t.Fatal(err)
	}
	owner := mkPID("node-1", "owner")
	pending, err := encode(pendingHeader{PID: owner.String(), Name: "claim", AttemptID: "before-join",
		RequiredNodes: []pid.NodeID{"node-1", "ghost"}, DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.Set(pendingKey("claim"), pending); err != nil {
		t.Fatal(err)
	}
	joiner := NewService(base, "node-2", nil, nil)
	joiner.ConfigureStrong(StrongDeps{IsLeader: func() bool { return false }})
	if err := joiner.StartReconciler(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !joiner.NameReady() {
		t.Fatal("joiner did not open admission after activation and seed")
	}
	if got, held := joiner.IsStrongReserved("claim"); !held || !got.Equal(owner) {
		t.Fatalf("pre-enrollment pending claim missing from seed: %v %v", got, held)
	}
	current, _, exists, err := joiner.strong.readParticipants()
	if err != nil || !exists || !contains(current.requiredNodes(), "node-2") {
		t.Fatalf("joiner did not commit participation: %+v %v", current, err)
	}
}

// A restart must not treat a stale local roster containing its old NodeID as
// an activation cutoff. It must observe this boot's new committed marker.
type staleParticipantSnapshot struct {
	kvapi.Engine
	old     kvapi.Entry
	release atomic.Bool
}

func (e *staleParticipantSnapshot) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	if len(keys) == 1 && keys[0] == participantsKey && !e.release.Load() {
		return map[string]kvapi.Entry{participantsKey: e.old}, 1, nil
	}
	return e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
}

func (e *staleParticipantSnapshot) ScanLocalSnapshot(prefix string, fn func(kvapi.Entry, uint64) bool) error {
	return e.Engine.(kvapi.LocalSnapshotScanner).ScanLocalSnapshot(prefix, fn)
}

func TestActivationWaitsForFreshLocalPublication(t *testing.T) {
	base := systemkv.NewService("activation-lag", nil)
	if _, err := base.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Stop(context.Background()) })
	value, err := encode(participantsValue{Nodes: []participantEntry{{Node: "node-1", Activation: "previous-boot"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.Set(participantsKey, value); err != nil {
		t.Fatal(err)
	}
	old, err := base.Get(participantsKey)
	if err != nil {
		t.Fatal(err)
	}
	lagged := &staleParticipantSnapshot{Engine: base, old: old}
	r := NewService(lagged, "node-1", nil, nil)
	r.ConfigureStrong(StrongDeps{})
	done := make(chan error, 1)
	go func() { done <- r.StartReconciler(t.Context()) }()
	if !eventually(t, time.Second, func() bool {
		entry, err := base.Get(participantsKey)
		return err == nil && entry.Version != old.Version
	}) {
		t.Fatal("fresh activation did not commit")
	}
	select {
	case err := <-done:
		t.Fatalf("startup passed stale activation snapshot: %v", err)
	default:
	}
	if r.NameReady() {
		t.Fatal("stale local roster opened name admission")
	}
	lagged.release.Store(true)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("activation did not finish after local publication caught up")
	}
	if !r.NameReady() {
		t.Fatal("fresh activation and seed did not open admission")
	}
}

func TestCorruptParticipantRosterFailsBeforeWatch(t *testing.T) {
	r := newStrongReg(t, nil, time.Second, nil)
	if _, err := r.engine.Set(participantsKey, []byte{0xc1}); err != nil {
		t.Fatal(err)
	}
	if err := r.StartReconciler(t.Context()); err == nil || !strings.Contains(err.Error(), participantsKey) {
		t.Fatalf("corrupt roster startup error: %v", err)
	}
	if r.NameReady() {
		t.Fatal("corrupt roster opened admission")
	}
}

func TestActivationRetriesLeaderChangeAndCancels(t *testing.T) {
	t.Run("recovers", func(t *testing.T) {
		r := newStrongReg(t, nil, time.Second, nil)
		flaky := &flakyActivationEngine{Engine: r.engine}
		flaky.remaining.Store(2)
		r.engine = flaky
		if err := r.StartReconciler(t.Context()); err != nil || !r.NameReady() {
			t.Fatalf("transient leader change prevented activation: ready=%v err=%v", r.NameReady(), err)
		}
	})
	t.Run("cancels", func(t *testing.T) {
		r := newStrongReg(t, nil, time.Second, nil)
		flaky := &flakyActivationEngine{Engine: r.engine}
		flaky.remaining.Store(1_000_000)
		r.engine = flaky
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- r.StartReconciler(ctx) }()
		if !eventually(t, time.Second, func() bool { return flaky.remaining.Load() < 1_000_000 }) {
			t.Fatal("activation did not attempt a write")
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) || r.NameReady() {
				t.Fatalf("canceled activation: ready=%v err=%v", r.NameReady(), err)
			}
		case <-time.After(time.Second):
			t.Fatal("activation ignored cancellation")
		}
	})
}

func TestActivationCancellationDoesNotWaitForBlockedTxn(t *testing.T) {
	r := newStrongReg(t, nil, time.Second, nil)
	blocked := &blockedActivationEngine{
		Engine:   r.engine,
		started:  make(chan struct{}, 1),
		release:  make(chan struct{}),
		finished: make(chan struct{}),
	}
	t.Cleanup(func() {
		close(blocked.release)
		if blocked.calls.Load() > 0 {
			<-blocked.finished
		}
	})
	r.engine = blocked
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.StartReconciler(ctx) }()
	select {
	case <-blocked.started:
	case <-time.After(time.Second):
		t.Fatal("activation never submitted")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("blocked submission returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("activation remained blocked after context deadline")
	}
	if r.NameReady() || blocked.calls.Load() != 1 {
		t.Fatalf("blocked submission opened admission or was retried: ready=%v calls=%d", r.NameReady(), blocked.calls.Load())
	}
}

func TestLateActivationCannotOverwriteRetry(t *testing.T) {
	r := newStrongReg(t, nil, time.Second, nil)
	blocked := &blockedActivationEngine{
		Engine:   r.engine,
		started:  make(chan struct{}, 1),
		release:  make(chan struct{}),
		finished: make(chan struct{}),
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(blocked.release) }) }
	t.Cleanup(release)
	r.engine = blocked
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.StartReconciler(ctx) }()
	select {
	case <-blocked.started:
	case <-time.After(time.Second):
		t.Fatal("first activation never submitted")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("first activation returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first activation did not time out")
	}
	if err := r.StartReconciler(t.Context()); err != nil {
		t.Fatalf("retry activation: %v", err)
	}
	activation := r.strong.activation
	release()
	select {
	case <-blocked.finished:
	case <-time.After(time.Second):
		t.Fatal("old submission did not finish")
	}
	roster, _, exists, err := r.strong.readParticipants()
	if err != nil || !exists || !roster.hasActivation(r.selfNode, activation) || !r.NameReady() {
		t.Fatalf("old submission superseded retry: roster=%+v ready=%v err=%v", roster, r.NameReady(), err)
	}
}
