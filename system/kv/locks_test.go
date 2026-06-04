// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"testing"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func newLockSvc(t *testing.T) *LockService {
	t.Helper()
	eng := NewService("lock", eventbus.NewBus(), zap.NewNop())
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })
	return NewLockService(eng, nil, "n1", zap.NewNop())
}

func TestLock_AcquireReleaseHolder(t *testing.T) {
	s := newLockSvc(t)
	a := pid.PID{Node: "n1", Host: "h", UniqID: "a"}
	b := pid.PID{Node: "n1", Host: "h", UniqID: "b"}

	if ok, err := s.Acquire("L", a); err != nil || !ok {
		t.Fatalf("a acquire: ok=%v err=%v", ok, err)
	}
	if ok, _ := s.Acquire("L", b); ok {
		t.Fatalf("b should not acquire a held lock")
	}
	if h, ok, _ := s.Holder("L"); !ok || h.String() != a.String() {
		t.Fatalf("holder = %v ok=%v, want a", h, ok)
	}
	if ok, _ := s.Release("L", b); ok {
		t.Fatalf("b (non-holder) must not release")
	}
	if ok, _ := s.Release("L", a); !ok {
		t.Fatalf("a (holder) must release")
	}
	if ok, err := s.Acquire("L", b); err != nil || !ok {
		t.Fatalf("b acquire after release: ok=%v err=%v", ok, err)
	}
}

func TestLock_ReapPID(t *testing.T) {
	s := newLockSvc(t)
	a := pid.PID{Node: "n1", Host: "h", UniqID: "a"}
	b := pid.PID{Node: "n1", Host: "h", UniqID: "b"}
	mustAcquire(t, s, "L1", a)
	mustAcquire(t, s, "L2", a)
	mustAcquire(t, s, "L3", b)

	s.ReapPID(a)

	if _, ok, _ := s.Holder("L1"); ok {
		t.Fatalf("L1 should be reaped")
	}
	if _, ok, _ := s.Holder("L2"); ok {
		t.Fatalf("L2 should be reaped")
	}
	if h, ok, _ := s.Holder("L3"); !ok || h.String() != b.String() {
		t.Fatalf("L3 should still be held by b")
	}
}

func TestLock_ReapNode(t *testing.T) {
	s := newLockSvc(t)
	x := pid.PID{Node: "X", Host: "h", UniqID: "1"}
	y := pid.PID{Node: "Y", Host: "h", UniqID: "1"}
	mustAcquire(t, s, "lx", x)
	mustAcquire(t, s, "ly", y)

	s.ReapNode("X")

	if _, ok, _ := s.Holder("lx"); ok {
		t.Fatalf("lx (node X) should be reaped")
	}
	if h, ok, _ := s.Holder("ly"); !ok || h.String() != y.String() {
		t.Fatalf("ly (node Y) should remain")
	}
}

func mustAcquire(t *testing.T, s *LockService, name string, holder pid.PID) {
	t.Helper()
	if ok, err := s.Acquire(name, holder); err != nil || !ok {
		t.Fatalf("acquire %s: ok=%v err=%v", name, ok, err)
	}
}
