// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// reapRaceEngine fires a hook exactly once, on the first delete-style call the
// reaper makes, so a test can interleave a release+reacquire between the
// reaper's scan and its delete. It overrides both delete variants so the same
// test detects a regression whether the reaper uses a guarded or unguarded
// delete.
type reapRaceEngine struct {
	kvapi.Engine
	hook func()
	once sync.Once
}

func (e *reapRaceEngine) fire() {
	if e.hook != nil {
		e.once.Do(e.hook)
	}
}

func (e *reapRaceEngine) Delete(key string) error {
	e.fire()
	return e.Engine.Delete(key)
}

func (e *reapRaceEngine) CompareAndDelete(key string, expect kvapi.Version) (bool, error) {
	e.fire()
	return e.Engine.CompareAndDelete(key, expect)
}

// TestLock_ReapDoesNotClobberReacquiredLock is the B1 regression: a reap whose
// scan observed a now-departed holder must not delete the lock once it has been
// re-acquired by a live holder on another node. The version captured at scan
// time makes the stale delete a no-op; an unconditional delete would destroy the
// live holder's lock and break mutual exclusion.
func TestLock_ReapDoesNotClobberReacquiredLock(t *testing.T) {
	eng := NewService("lock", eventbus.NewBus(), zap.NewNop())
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })

	dead := pid.PID{Node: "dead", Host: "h", UniqID: "a"}
	live := pid.PID{Node: "live", Host: "h", UniqID: "b"}

	hooked := &reapRaceEngine{Engine: eng}
	s := NewLockService(hooked, nil, "n1", zap.NewNop())

	if ok, err := s.Acquire("L", dead); err != nil || !ok {
		t.Fatalf("dead acquire: ok=%v err=%v", ok, err)
	}
	held, err := eng.Get(lockKey("L"))
	if err != nil {
		t.Fatalf("get held lock: %v", err)
	}
	deadVersion := held.Version

	hooked.hook = func() {
		if ok, err := eng.CompareAndDelete(lockKey("L"), deadVersion); err != nil || !ok {
			t.Errorf("simulated reap of dead holder: ok=%v err=%v", ok, err)
		}
		if _, stored, err := eng.SetIfAbsent(lockKey("L"), []byte(live.String())); err != nil || !stored {
			t.Errorf("simulated reacquire by live holder: stored=%v err=%v", stored, err)
		}
	}

	s.ReapNode("dead")

	h, ok, err := s.Holder("L")
	if err != nil {
		t.Fatalf("holder: %v", err)
	}
	if !ok || h.String() != live.String() {
		t.Fatalf("stale reap clobbered the reacquired lock: holder=%v ok=%v, want live %v", h, ok, live)
	}
}
