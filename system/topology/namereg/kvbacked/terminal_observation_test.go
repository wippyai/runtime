// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"testing"
	"time"
)

type delayedSnapshotEngine struct {
	kvapi.Engine
	afterRead func()
}

func (e *delayedSnapshotEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	entries, index, err := e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
	if e.afterRead != nil {
		e.afterRead()
	}
	return entries, index, err
}

func TestDelayedAbsenceCannotRetireReplacement(t *testing.T) {
	for _, installExclusion := range []bool{false, true} {
		label := "new-waiter-and-timer"
		if installExclusion {
			label = "new-exclusion"
		}
		t.Run(label, func(t *testing.T) {
			r := newStrongReg(t, nil, 0, nil)
			owner := mkPID("node-1", "new")
			waiter := &strongWaiter{ch: make(chan globalapi.RegisterOutcome, 1)}
			timer := &strongTimer{owner: r.strong, timer: time.AfterFunc(time.Hour, func() {}), done: make(chan struct{})}
			defer timer.Stop()
			r.engine = &delayedSnapshotEngine{Engine: r.engine, afterRead: func() {
				if installExclusion {
					if _, err := r.strong.learnExclusion("name", owner, 2, exclusionActive); err != nil {
						t.Fatal(err)
					}
				}
				r.strong.addWaiter("name", waiter)
				r.strong.mu.Lock()
				r.strong.timers["name"] = timer
				r.strong.mu.Unlock()
			}}
			if err := r.strong.reconcile("name"); err != nil {
				t.Fatal(err)
			}
			if installExclusion {
				got, held := r.IsStrongReserved("name")
				if !held || !got.Equal(owner) {
					t.Fatal("delayed absence removed replacement exclusion")
				}
			}
			select {
			case <-waiter.ch:
				t.Fatal("delayed absence notified a new waiter")
			default:
			}
			r.strong.mu.Lock()
			current := r.strong.timers["name"]
			r.strong.mu.Unlock()
			if current != timer || !timer.Stop() {
				t.Fatal("delayed absence stopped a new timer")
			}
		})
	}
}

func TestCurrentAbsenceRetiresObservedWaiter(t *testing.T) {
	r := newStrongReg(t, nil, 0, nil)
	waiter := &strongWaiter{ch: make(chan globalapi.RegisterOutcome, 1)}
	r.strong.addWaiter("name", waiter)
	if err := r.strong.reconcile("name"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-waiter.ch:
		if result.State != globalapi.RegisterStateExpired {
			t.Fatal("wrong terminal result")
		}
	default:
		t.Fatal("observed waiter was not notified")
	}
}

type withoutSnapshotEngine struct{ kvapi.Engine }

func TestStartupRequiresAtomicSnapshotCapabilityEvenWhenEmpty(t *testing.T) {
	r := newStrongReg(t, nil, 0, nil)
	r.engine = withoutSnapshotEngine{r.engine}
	if err := r.StartReconciler(context.Background()); err == nil {
		t.Fatal("unsupported engine admitted naming startup")
	}
	if r.NameReady() {
		t.Fatal("unsupported engine became ready")
	}
}
