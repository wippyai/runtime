// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"fmt"
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

// promotionBetweenReadsEngine models the old two-Get race. The first local
// snapshot is captured while the name is pending, then promotion is committed
// before the caller consumes that snapshot. A reconciler that separately read
// active and pending could observe active absent and pending absent and clear a
// still-held exclusion. The snapshot reader must keep the pending observation.
type promotionBetweenReadsEngine struct {
	kvapi.Engine
	promoted bool
}

type unsupportedSnapshotEngine struct {
	kvapi.Engine
	watchCalls atomic.Int32
}

func (e *unsupportedSnapshotEngine) Watch(ctx context.Context, prefix string) (kvapi.Watcher, error) {
	e.watchCalls.Add(1)
	return e.Engine.Watch(ctx, prefix)
}

type failingSnapshotEngine struct {
	kvapi.Engine
	err error
}

func (e *failingSnapshotEngine) ReadLocalSnapshot([]string) (map[string]kvapi.Entry, uint64, error) {
	return nil, 0, e.err
}

func (e *failingSnapshotEngine) ScanLocalSnapshot(string, func(kvapi.Entry, uint64) bool) error {
	return e.err
}

type toggleSnapshotEngine struct {
	kvapi.Engine
	watcher kvapi.Watcher
	failed  atomic.Bool
}

func (e *toggleSnapshotEngine) Watch(ctx context.Context, prefix string) (kvapi.Watcher, error) {
	if e.watcher != nil {
		return e.watcher, nil
	}
	return e.Engine.Watch(ctx, prefix)
}

func (e *toggleSnapshotEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	if e.failed.Load() {
		return nil, 0, errors.New("snapshot unavailable")
	}
	reader, ok := e.Engine.(kvapi.LocalSnapshotReader)
	if !ok {
		return nil, 0, kvapi.ErrKVClosed
	}
	return reader.ReadLocalSnapshot(keys)
}

func (e *toggleSnapshotEngine) ScanLocalSnapshot(prefix string, fn func(kvapi.Entry, uint64) bool) error {
	if e.failed.Load() {
		return errors.New("snapshot unavailable")
	}
	return e.Engine.(kvapi.LocalSnapshotScanner).ScanLocalSnapshot(prefix, fn)
}

func putPendingSnapshotRecord(t *testing.T, engine kvapi.Engine, name string, owner pid.PID, required []pid.NodeID) kvapi.Entry {
	t.Helper()
	value, err := encode(pendingHeader{PID: owner.String(), Name: name, AttemptID: "attempt-snapshot", RequiredNodes: required, DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Set(pendingKey(name), value); err != nil {
		t.Fatal(err)
	}
	entry, err := engine.Get(pendingKey(name))
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func (e *promotionBetweenReadsEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	reader, ok := e.Engine.(kvapi.LocalSnapshotReader)
	if !ok {
		return nil, 0, kvapi.ErrKVClosed
	}
	entries, revision, err := reader.ReadLocalSnapshot(keys)
	if err == nil && !e.promoted {
		if pending, found := entries[pendingKey("claim")]; found {
			e.promoted = true
			if err := e.promote(pending); err != nil {
				return nil, 0, err
			}
		}
	}
	return entries, revision, err
}

func ownerFromPending(entry kvapi.Entry) (pendingHeader, error) {
	hdr, err := decodePending(entry.Value)
	if err != nil {
		return pendingHeader{}, err
	}
	return hdr, nil
}

func (e *promotionBetweenReadsEngine) promote(pending kvapi.Entry) error {
	hdr, err := ownerFromPending(pending)
	if err != nil {
		return err
	}
	active, err := encode(activeValue{PID: hdr.PID, Name: "claim", AttemptID: hdr.AttemptID, Strong: true})
	if err != nil {
		return err
	}
	ok, err := e.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: pendingKey("claim"), Expect: pending.Version},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: pendingKey("claim")},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: activeKey("claim"), Value: active},
	})
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("promotion transaction did not commit")
	}
	return nil
}

func (e *promotionBetweenReadsEngine) Get(key string) (kvapi.Entry, error) {
	entry, err := e.Engine.Get(key)
	if key == activeKey("claim") && err != nil {
		// This is the exact old ordering: return the absent active result, then
		// make the pending record disappear before the second Get.
		if !e.promoted {
			pending, pendingErr := e.Engine.Get(pendingKey("claim"))
			if pendingErr != nil {
				return entry, err
			}
			e.promoted = true
			if promoteErr := e.promote(pending); promoteErr != nil {
				return entry, promoteErr
			}
		}
	}
	return entry, err
}

func TestReconcileUsesOneSnapshotAcrossPromotion(t *testing.T) {
	engine := systemkv.NewService("snapshot-race", nil)
	if _, err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer engine.Stop(context.Background())

	owner := mkPID("node-1", "owner")
	hdr, err := encode(pendingHeader{PID: owner.String(), Name: "claim", AttemptID: "attempt-promotion", RequiredNodes: []pid.NodeID{"node-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Set(pendingKey("claim"), hdr); err != nil {
		t.Fatal(err)
	}

	r := NewService(engine, "node-1", nil, nil)
	r.ConfigureStrong(StrongDeps{
		Membership: func() []pid.NodeID { return []pid.NodeID{"node-1"} },
		IsLeader:   func() bool { return false },
		Deadline:   time.Second,
	})
	pending, err := engine.Get(pendingKey("claim"))
	if err != nil {
		t.Fatal(err)
	}
	r.strong.latch("claim", "attempt-promotion", owner, pending.Epoch)

	raced := &promotionBetweenReadsEngine{Engine: engine}
	r.engine = raced
	r.localRead = raced
	if report := r.strong.reconcile("claim"); report.err != nil {
		t.Fatalf("reconcile: %v", report.err)
	}
	if _, ok := r.IsStrongReserved("claim"); !ok {
		t.Fatal("coherent pending observation cleared the held exclusion")
	}
	if _, err := engine.Get(activeKey("claim")); err != nil {
		t.Fatalf("promotion after snapshot did not commit: %v", err)
	}

	if report := r.strong.reconcile("claim"); report.err != nil {
		t.Fatalf("reconcile promoted record: %v", report.err)
	}
	got, ok := r.IsStrongReserved("claim")
	if !ok || got.String() != owner.String() {
		t.Fatalf("active promotion lost exclusion: owner=%v reserved=%v", got, ok)
	}
}

func TestStrongStartRejectsUnsupportedSnapshotBeforeWatch(t *testing.T) {
	engine := systemkv.NewService("unsupported-snapshot", nil)
	if _, err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer engine.Stop(context.Background())

	wrapped := &unsupportedSnapshotEngine{Engine: engine}
	r := NewService(wrapped, "node-1", nil, nil)
	r.ConfigureStrong(StrongDeps{Membership: func() []pid.NodeID { return []pid.NodeID{"node-1"} }})
	if err := r.StartReconciler(context.Background()); err == nil || !strings.Contains(err.Error(), "coherent local KV snapshots") {
		t.Fatalf("unsupported engine startup error=%v", err)
	}
	if wrapped.watchCalls.Load() != 0 {
		t.Fatal("unsupported engine opened a watch before capability validation")
	}
}

func TestNonMemberSnapshotCapability(t *testing.T) {
	t.Run("strong_requires_replica", func(t *testing.T) {
		r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
		r.SetNonMember(func() bool { return true })
		wrapped := &unsupportedSnapshotEngine{Engine: r.engine}
		r.engine = wrapped
		if err := r.StartReconciler(t.Context()); err == nil || !strings.Contains(err.Error(), "without a local replica") {
			t.Fatalf("non-member Strong startup: %v", err)
		}
		if wrapped.watchCalls.Load() != 0 || r.NameReady() {
			t.Fatal("unsupported Strong participant started reconciliation")
		}
		if _, err := r.RegisterScope(t.Context(), "claim", mkPID("node-1", "owner"), globalapi.Strong); err == nil {
			t.Fatal("unsupported Strong participant accepted registration")
		}
		if _, err := wrapped.Get(pendingKey("claim")); !errors.Is(err, kvapi.ErrKeyNotFound) {
			t.Fatalf("rejected registration created a pending claim: %v", err)
		}
	})
	t.Run("dissemination_does_not_require_snapshot", func(t *testing.T) {
		r, _ := newDissemReg(t, "client")
		r.SetNonMember(func() bool { return true })
		wrapped := &unsupportedSnapshotEngine{Engine: r.engine}
		r.engine = wrapped
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		if err := r.StartReconciler(ctx); err != nil {
			t.Fatalf("non-member dissemination startup: %v", err)
		}
		if wrapped.watchCalls.Load() != 1 || !r.ready.Load() || !r.NameReady() {
			t.Fatal("dissemination-only client did not become ready")
		}
		cancel()
		if !eventually(t, time.Second, func() bool { return !r.ready.Load() }) {
			t.Fatal("canceled client did not stop reconciliation")
		}
	})
}

func TestStrongInvalidSnapshotRecordPreservesObligations(t *testing.T) {
	owner := mkPID("node-1", "owner")
	badNameActive, err := encode(activeValue{Name: "wrong", PID: owner.String(), AttemptID: "attempt-invalid", Strong: true})
	if err != nil {
		t.Fatal(err)
	}
	badOwnerActive, err := encode(activeValue{Name: "claim", PID: "invalid", AttemptID: "attempt-invalid", Strong: true})
	if err != nil {
		t.Fatal(err)
	}
	badNamePending, err := encode(pendingHeader{Name: "wrong", PID: owner.String(), AttemptID: "attempt-invalid"})
	if err != nil {
		t.Fatal(err)
	}
	badOwnerPending, err := encode(pendingHeader{Name: "claim", PID: "invalid", AttemptID: "attempt-invalid"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, key string
		value     []byte
	}{
		{name: "active_encoding", key: activeKey("claim"), value: []byte{0xc1}},
		{name: "active_name", key: activeKey("claim"), value: badNameActive},
		{name: "active_owner", key: activeKey("claim"), value: badOwnerActive},
		{name: "pending_encoding", key: pendingKey("claim"), value: []byte{0xc1}},
		{name: "pending_name", key: pendingKey("claim"), value: badNamePending},
		{name: "pending_owner", key: pendingKey("claim"), value: badOwnerPending},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
			if _, err := r.engine.Set(tc.key, tc.value); err != nil {
				t.Fatal(err)
			}
			r.strong.latch("claim", "attempt-invalid", owner, 1)
			waiter := &strongWaiter{ch: make(chan strongCompletion, 1), attemptID: "attempt-invalid"}
			r.strong.addWaiter("claim", waiter)
			if report := r.strong.reconcile("claim"); report.err == nil {
				t.Fatal("invalid record was mistaken for an absent claim")
			}
			if got, held := r.IsStrongReserved("claim"); !held || !got.Equal(owner) {
				t.Fatalf("invalid record released exclusion: owner=%v held=%v", got, held)
			}
			select {
			case result := <-waiter.ch:
				t.Fatalf("invalid record delivered terminal outcome: %+v", result)
			default:
			}
		})
	}
}

type pausedSnapshotEngine struct {
	kvapi.Engine
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *pausedSnapshotEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	reader := e.Engine.(kvapi.LocalSnapshotReader)
	entries, revision, err := reader.ReadLocalSnapshot(keys)
	e.once.Do(func() { close(e.entered) })
	<-e.release
	return entries, revision, err
}

func TestOldSnapshotAndFailureCannotChangeReplacementOwner(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	oldCtx, cancelOld := context.WithCancel(t.Context())
	defer cancelOld()
	old := &reconcilerLifecycle{ctx: oldCtx, cancel: cancelOld}
	r.reconciler.Store(old)
	r.ready.Store(true)
	owner := mkPID("node-1", "owner")
	r.strong.latch("claim", "attempt-old", owner, 1)
	waiter := &strongWaiter{ch: make(chan strongCompletion, 1), attemptID: "attempt-old"}
	r.strong.addWaiter("claim", waiter)
	paused := &pausedSnapshotEngine{Engine: r.engine, entered: make(chan struct{}), release: make(chan struct{})}
	r.engine = paused
	r.localRead = paused
	result := make(chan reconcileReport, 1)
	go func() { result <- r.strong.reconcileForRun("claim", old) }()
	select {
	case <-paused.entered:
	case <-time.After(time.Second):
		t.Fatal("snapshot was not captured")
	}
	cancelOld()
	newCtx, cancelNew := context.WithCancel(t.Context())
	defer cancelNew()
	replacement := &reconcilerLifecycle{ctx: newCtx, cancel: cancelNew}
	replacement.watch.Store(&reconcilerWatch{Watcher: &readinessWatcher{events: make(chan kvapi.WatchEvent), closed: make(chan struct{})}})
	r.reconcilerMu.Lock()
	r.reconciler.Store(replacement)
	r.ready.Store(true)
	r.reconcilerMu.Unlock()
	close(paused.release)
	select {
	case report := <-result:
		if report.err != nil {
			t.Fatalf("old snapshot error=%v", report.err)
		}
	case <-time.After(time.Second):
		t.Fatal("old snapshot did not finish")
	}
	r.failReconciler(old, errors.New("delayed old failure"))
	if newCtx.Err() != nil || !r.NameReady() {
		t.Fatal("old owner error canceled replacement owner")
	}
	if got, held := r.IsStrongReserved("claim"); !held || !got.Equal(owner) {
		t.Fatal("old empty snapshot cleared exclusion")
	}
	select {
	case out := <-waiter.ch:
		t.Fatalf("old snapshot completed replacement waiter: %+v", out)
	default:
	}
}

func TestStrongSnapshotErrorDirectPreservesExclusion(t *testing.T) {
	engine := systemkv.NewService("direct-snapshot-error", nil)
	if _, err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer engine.Stop(context.Background())

	owner := mkPID("node-1", "owner")
	pending := putPendingSnapshotRecord(t, engine, "claim", owner, []pid.NodeID{"node-1"})
	r := NewService(&failingSnapshotEngine{Engine: engine, err: errors.New("snapshot unavailable")}, "node-1", nil, nil)
	r.ConfigureStrong(StrongDeps{Membership: func() []pid.NodeID { return []pid.NodeID{"node-1"} }})
	r.strong.latch("claim", "attempt-snapshot", owner, pending.Epoch)
	if report := r.strong.reconcile("claim"); report.err == nil || !strings.Contains(report.err.Error(), "snapshot unavailable") {
		t.Fatalf("direct snapshot failure error=%v", report.err)
	}
	if got, ok := r.IsStrongReserved("claim"); !ok || got.String() != owner.String() {
		t.Fatalf("direct failure cleared exclusion: owner=%v reserved=%v", got, ok)
	}
}

func TestStrongStartupSnapshotErrorPreservesExclusion(t *testing.T) {
	engine := systemkv.NewService("startup-snapshot-error", nil)
	if _, err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer engine.Stop(context.Background())
	owner := mkPID("node-1", "owner")
	pending := putPendingSnapshotRecord(t, engine, "claim", owner, []pid.NodeID{"node-1"})
	r := NewService(&failingSnapshotEngine{Engine: engine, err: errors.New("snapshot unavailable")}, "node-1", nil, nil)
	r.ConfigureStrong(StrongDeps{Membership: func() []pid.NodeID { return []pid.NodeID{"node-1"} }})
	r.strong.latch("claim", "attempt-snapshot", owner, pending.Epoch)
	if err := r.StartReconciler(context.Background()); err == nil || !strings.Contains(err.Error(), "snapshot unavailable") {
		t.Fatalf("startup snapshot failure error=%v", err)
	}
	if r.NameReady() {
		t.Fatal("startup snapshot failure opened readiness")
	}
	if got, ok := r.IsStrongReserved("claim"); !ok || got.String() != owner.String() {
		t.Fatalf("startup failure cleared exclusion: owner=%v reserved=%v", got, ok)
	}
}

func TestStrongWatchSnapshotErrorStopsAdmission(t *testing.T) {
	engine := systemkv.NewService("watch-snapshot-error", nil)
	if _, err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer engine.Stop(context.Background())
	owner := mkPID("node-1", "owner")
	putPendingSnapshotRecord(t, engine, "claim", owner, []pid.NodeID{"node-1", "ghost"})
	wrapped := &toggleSnapshotEngine{Engine: engine}
	r := NewService(wrapped, "node-1", nil, nil)
	r.ConfigureStrong(StrongDeps{
		Membership: func() []pid.NodeID { return []pid.NodeID{"node-1", "ghost"} },
		IsLeader:   func() bool { return false },
		Deadline:   time.Second,
	})
	if err := r.StartReconciler(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !r.NameReady() {
		t.Fatal("healthy snapshot reader did not open readiness")
	}
	if got, ok := r.IsStrongReserved("claim"); !ok || got.String() != owner.String() {
		t.Fatalf("seed did not preserve exclusion: owner=%v reserved=%v", got, ok)
	}
	result := make(chan error, 1)
	go func() {
		_, err := r.RegisterScope(context.Background(), "inflight", mkPID("node-1", "inflight"), globalapi.Strong)
		result <- err
	}()
	if !eventually(t, time.Second, func() bool { _, ok := r.IsStrongReserved("inflight"); return ok }) {
		t.Fatal("in-flight Strong claim did not establish its exclusion")
	}
	wrapped.failed.Store(true)
	if _, err := engine.Set(ackKey("claim", "attempt-snapshot", "ghost"), []byte("ghost")); err != nil {
		t.Fatal(err)
	}
	if !eventually(t, time.Second, func() bool { return !r.NameReady() }) {
		t.Fatal("watch snapshot error left readiness open")
	}
	if _, ok := r.IsStrongReserved("claim"); !ok {
		t.Fatal("watch snapshot error cleared exclusion")
	}
	select {
	case err := <-result:
		if !errors.Is(err, globalapi.ErrNotReady) {
			t.Fatalf("in-flight claim error after synchronization failure=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight Strong claim did not observe owner cancellation")
	}
	if _, err := engine.Get(pendingKey("inflight")); err != nil {
		t.Fatalf("synchronization failure removed another claim: %v", err)
	}
	if _, err := r.RegisterScope(context.Background(), "new", mkPID("node-1", "new"), globalapi.Strong); !errors.Is(err, globalapi.ErrNotReady) {
		t.Fatalf("post-failure Strong admission error=%v", err)
	}
}

func TestStrongSweepSnapshotErrorStopsAdmission(t *testing.T) {
	engine := systemkv.NewService("sweep-snapshot-error", nil)
	if _, err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer engine.Stop(context.Background())
	owner := mkPID("node-1", "owner")
	putPendingSnapshotRecord(t, engine, "claim", owner, []pid.NodeID{"node-1", "ghost"})
	// No watch events: a recovery scan must observe the injected read failure.
	// The pending deadline is in the future, so seed cannot expire it.
	watcher := &readinessWatcher{events: make(chan kvapi.WatchEvent), closed: make(chan struct{})}
	wrapped := &toggleSnapshotEngine{Engine: engine, watcher: watcher}
	r := NewService(wrapped, "node-1", nil, nil)
	r.ConfigureStrong(StrongDeps{
		Membership: func() []pid.NodeID { return []pid.NodeID{"node-1", "ghost"} },
		IsLeader:   func() bool { return true },
		Deadline:   time.Second,
	})
	if err := r.StartReconciler(context.Background()); err != nil {
		t.Fatal(err)
	}
	wrapped.failed.Store(true)
	r.strong.owner.Load().requestScan()
	if !eventually(t, 2*time.Second, func() bool { return !r.NameReady() }) {
		t.Fatal("recovery scan snapshot error left readiness open")
	}
	select {
	case <-watcher.closed:
	case <-time.After(time.Second):
		t.Fatal("failed sweep left watch owner running")
	}
	if got, held := r.IsStrongReserved("claim"); !held || !got.Equal(owner) {
		t.Fatal("failed sweep released exclusion")
	}
	if _, err := r.RegisterScope(context.Background(), "new", mkPID("node-1", "new"), globalapi.Strong); !errors.Is(err, globalapi.ErrNotReady) {
		t.Fatalf("post-sweep-failure Strong admission error=%v", err)
	}
}
