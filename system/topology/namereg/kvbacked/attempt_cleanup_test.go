// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

// pausedTxnEngine makes the transaction continuation race deterministic. The
// transaction commits before its continuation waits for the replacement.
type pausedTxnEngine struct {
	kvapi.Engine
	match   func([]kvapi.TxnOp) bool
	after   func()
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *pausedTxnEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	reader, ok := e.Engine.(kvapi.LocalSnapshotReader)
	if !ok {
		return nil, 0, kvapi.ErrKVClosed
	}
	return reader.ReadLocalSnapshot(keys)
}

func (e *pausedTxnEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	committed, err := e.Engine.Txn(ops)
	if committed && e.match(ops) {
		e.once.Do(func() {
			close(e.entered)
			if e.after != nil {
				e.after()
			}
			<-e.release
		})
	}
	return committed, err
}

func putStrongPending(t *testing.T, engine kvapi.Engine, hdr pendingHeader) kvapi.Entry {
	t.Helper()
	value, err := encode(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Set(pendingKey(hdr.Name), value); err != nil {
		t.Fatal(err)
	}
	entry, err := engine.Get(pendingKey(hdr.Name))
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func installReplacementTimer(t *testing.T, r *Service, base kvapi.Engine, attempt string, required []pid.NodeID) kvapi.Entry {
	t.Helper()
	p := mkPID("node-1", attempt)
	replacement := putStrongPending(t, base, pendingHeader{
		PID:              p.String(),
		Name:             "claim",
		AttemptID:        attempt,
		RequiredNodes:    required,
		DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano(),
	})
	r.strong.armTimer("claim", attempt, replacement.Epoch, replacement.Version, time.Now().Add(time.Minute).UnixNano())
	return replacement
}

func hasTimer(r *Service, name, attempt string) bool {
	r.strong.mu.Lock()
	defer r.strong.mu.Unlock()
	t, ok := r.strong.timers[name]
	return ok && t.attemptID == attempt
}

func TestStrongPromotionContinuationCannotStopReplacementTimer(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "peer"}, time.Second, nil)
	base := r.engine
	owner := mkPID("node-1", "old")
	old := putStrongPending(t, base, pendingHeader{
		PID: owner.String(), Name: "claim", AttemptID: "old", RequiredNodes: []pid.NodeID{"node-1"},
		DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano(),
	})
	if _, err := base.Set(ackKey("claim", "old", "node-1"), []byte("node-1")); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	r.engine = &pausedTxnEngine{
		Engine: base,
		match: func(ops []kvapi.TxnOp) bool {
			for _, op := range ops {
				if op.Kind == kvapi.TxnPut && op.Key == activeKey("claim") {
					return true
				}
			}
			return false
		},
		after: func() {
			if err := base.Delete(activeKey("claim")); err != nil {
				t.Fatal(err)
			}
			installReplacementTimer(t, r, base, "replacement", []pid.NodeID{"node-1", "peer"})
			r.strong.addWaiter("claim", &strongWaiter{
				ch: make(chan globalapi.RegisterOutcome, 1), attemptID: "replacement", pid: mkPID("node-1", "replacement"),
			})
			r.strong.setTerminal("claim", "replacement", "deadline", nil, 99)
		},
		entered: entered,
		release: release,
	}
	done := make(chan struct{})
	go func() {
		r.strong.leaderPromote("claim", old.Epoch, old.Version, pendingHeader{
			PID: owner.String(), Name: "claim", AttemptID: "old", RequiredNodes: []pid.NodeID{"node-1"},
		})
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("promotion transaction did not pause")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("promotion continuation did not finish")
	}
	if !hasTimer(r, "claim", "replacement") {
		r.strong.mu.Lock()
		got := r.strong.timers["claim"]
		r.strong.mu.Unlock()
		t.Fatalf("old promotion stopped the replacement timer: %+v", got)
	}
	if reason, _, epoch := r.strong.takeTerminal("replacement"); reason != "deadline" || epoch != 99 {
		t.Fatalf("old promotion removed replacement terminal details: reason=%q epoch=%d", reason, epoch)
	}
	r.strong.stopTimer("claim", "replacement")
}

func TestStrongExpiryContinuationCannotStopReplacementTimer(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "peer"}, time.Second, nil)
	base := r.engine
	owner := mkPID("node-1", "old")
	hdr := pendingHeader{
		PID: owner.String(), Name: "claim", AttemptID: "old", RequiredNodes: []pid.NodeID{"node-1", "peer"},
		DeadlineUnixNano: time.Now().Add(-time.Minute).UnixNano(),
	}
	old := putStrongPending(t, base, hdr)
	r.strong.latch("claim", owner, hdr.AttemptID, old.Epoch)
	entered := make(chan struct{})
	release := make(chan struct{})
	r.engine = &pausedTxnEngine{
		Engine: base,
		match: func(ops []kvapi.TxnOp) bool {
			for _, op := range ops {
				if op.Kind == kvapi.TxnDelete && op.Key == pendingKey("claim") {
					return true
				}
			}
			return false
		},
		after: func() {
			installReplacementTimer(t, r, base, "replacement", []pid.NodeID{"node-1", "peer"})
			r.strong.addWaiter("claim", &strongWaiter{
				ch: make(chan globalapi.RegisterOutcome, 1), attemptID: "replacement", pid: mkPID("node-1", "replacement"),
			})
			r.strong.setTerminal("claim", "replacement", "deadline", nil, 99)
		},
		entered: entered,
		release: release,
	}
	done := make(chan struct{})
	go func() {
		r.strong.leaderExpire("claim", old.Epoch, old.Version, hdr, "deadline")
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("expiry transaction did not pause")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expiry continuation did not finish")
	}
	if !hasTimer(r, "claim", "replacement") {
		t.Fatal("old expiry stopped the replacement timer")
	}
	if reason, _, epoch := r.strong.takeTerminal("replacement"); reason != "deadline" || epoch != 99 {
		t.Fatalf("old expiry removed replacement terminal details: reason=%q epoch=%d", reason, epoch)
	}
	r.strong.stopTimer("claim", "replacement")
}

func TestStrongActivePromotionStopsPendingTimerByAttempt(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	r.strong.armTimer("claim", "attempt", 10, 10, time.Now().Add(time.Minute).UnixNano())
	r.strong.onActive("claim", 11, "attempt", mkPID("node-1", "owner"))
	if hasTimer(r, "claim", "attempt") {
		t.Fatal("active promotion left the pending timer armed")
	}
}

func TestStrongOldDriverCannotReplaceNewerTimer(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	r.strong.armTimer("claim", "new", 2, 2, time.Now().Add(time.Minute).UnixNano())
	p := mkPID("node-1", "old")
	r.strong.leaderDrive("claim", 1, 1, pendingHeader{
		PID: p.String(), Name: "claim", AttemptID: "old",
		RequiredNodes: []pid.NodeID{"node-1", "peer"}, DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano(),
	})
	if !hasTimer(r, "claim", "new") {
		t.Fatal("old driver replaced the newer timer")
	}
	r.strong.stopTimer("claim", "new")
}

func TestStrongLocalKVVersionOrdersTimersWithoutRaftEpoch(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	deadline := time.Now().Add(time.Minute).UnixNano()
	r.strong.armTimer("claim", "new", 0, 12, deadline)
	r.strong.armTimer("claim", "old", 0, 11, deadline)
	if !hasTimer(r, "claim", "new") {
		t.Fatal("older local-KV observation replaced the newer deadline timer")
	}
	r.strong.stopTimer("claim", "new")
}
