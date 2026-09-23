// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/system/topology"
	"github.com/wippyai/runtime/system/topology/namereg/admission"
)

// The ACK for B uses a newer coherent KV observation than A's committed
// events. Delayed delivery of A's active PUT and DELETE must not undo B's
// exclusion or its retry obligation, including when Entry.Epoch is zero.
func TestOlderActiveEventsCannotReplaceNewerPendingExclusion(t *testing.T) {
	gate := &admission.Coordinator{}
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, time.Minute, nil)
	r.ConfigureStrong(StrongDeps{
		Admission:  gate,
		IsLeader:   func() bool { return false },
		Deadline:   time.Minute,
	})
	local := topology.NewPIDRegistry(topology.WithGlobalRegistry(r), topology.WithAdmissionCoordinator(gate))
	// Drive the ordered observer explicitly so no background worker repairs a
	// lost exclusion between the two deliberately delayed watch events.
	run := &reconcilerLifecycle{ctx: t.Context()}
	run.watch.Store(&reconcilerWatch{Watcher: &readinessWatcher{
		events: make(chan kvapi.WatchEvent), closed: make(chan struct{}),
	}})
	r.reconciler.Store(run)
	r.ready.Store(true)
	t.Cleanup(func() { r.strong.stopTimer("reused") })
	const name = "reused"
	const attemptA = "000000000000000000000000000000aa"
	const attemptB = "000000000000000000000000000000bb"
	first := mkPID("node-1", "first")
	second := mkPID("node-1", "second")
	valueA, err := encode(activeValue{PID: first.String(), Name: name, AttemptID: attemptA, Strong: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(activeKey(name), valueA); err != nil {
		t.Fatal(err)
	}
	_, oldRevision, err := r.localRead.ReadLocalSnapshot([]string{activeKey(name)})
	if err != nil || oldRevision == 0 {
		t.Fatalf("old active revision=%d err=%v", oldRevision, err)
	}
	if err := r.engine.Delete(activeKey(name)); err != nil {
		t.Fatal(err)
	}
	valueB, err := encode(pendingHeader{PID: second.String(), Name: name,
		AttemptID: attemptB, RequiredNodes: []pid.NodeID{"node-1", "ghost"},
		DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(pendingKey(name), valueB); err != nil {
		t.Fatal(err)
	}
	pending, err := r.engine.Get(pendingKey(name))
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := r.strong.attest(name, pending.Epoch, pending.Version, attemptB, second,
		[]pid.NodeID{"node-1", "ghost"}); err != nil || !ok {
		t.Fatalf("new pending vote failed: %v", err)
	}
	if _, err := r.engine.Get(ackKey(name, attemptB, "node-1")); err != nil {
		t.Fatal("new ACK was not committed:", err)
	}
	r.strong.armTimerAttempt(name, attemptB, time.Now().Add(time.Minute).UnixNano())
	r.strong.mu.Lock()
	newRevision := r.strong.exclusions[name].observed
	wake := r.strong.timers[name]
	r.strong.mu.Unlock()
	if newRevision <= oldRevision || wake == nil {
		t.Fatalf("new exclusion revision=%d old=%d timer=%v", newRevision, oldRevision, wake)
	}
	r.handleWatchEvent(kvapi.WatchEvent{Type: kvapi.WatchPut, Revision: oldRevision,
		Current: &kvapi.Entry{Key: activeKey(name), Value: valueA}})
	r.handleWatchEvent(kvapi.WatchEvent{Type: kvapi.WatchDelete, Revision: oldRevision + 1,
		Previous: &kvapi.Entry{Key: activeKey(name), Value: valueA}})
	if got, ok := r.IsStrongReserved(name); !ok || !got.Equal(second) {
		t.Fatalf("old active events replaced newer pending exclusion: %v %v", got, ok)
	}
	r.strong.mu.Lock()
	current := r.strong.timers[name]
	r.strong.mu.Unlock()
	if current != wake {
		t.Fatal("old active transition canceled the newer attempt timer")
	}
	if _, err := local.Register(name, mkPID("node-1", "competitor")); err == nil {
		t.Fatal("LOCAL admitted a competing owner after the new Strong ACK")
	}
	if _, err := local.Register("independent", mkPID("node-1", "independent")); err != nil {
		t.Fatalf("unrelated LOCAL admission failed: %v", err)
	}
}
