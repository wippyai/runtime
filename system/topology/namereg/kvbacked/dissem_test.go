// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"
	"time"

	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/eventbus"
	systemkv "github.com/wippyai/runtime/system/kv"
	"github.com/wippyai/runtime/system/topology/namereg/global"
	"go.uber.org/zap"
)

func newDissemReg(t *testing.T, node string) (*Service, *global.Dissem) {
	t.Helper()
	eng := systemkv.NewService(node, eventbus.NewBus(), nil)
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })
	d := global.NewDissem(node, zap.NewNop())
	s := NewService(eng, node, nil, nil)
	s.ConfigureDissem(d)
	return s, d
}

func pump(from, to *global.Dissem) {
	for _, frame := range from.GetBroadcasts(0, 1<<20) {
		to.NotifyMsg(frame)
	}
}

// TestDissem_NonMemberResolvesViaGossip proves a node with an empty engine (a
// non-member with no raft FSM) resolves an active name purely from the gossiped
// dissem cache, and stops resolving it once a tombstone gossips through.
func TestDissem_NonMemberResolvesViaGossip(t *testing.T) {
	svcA, dissemA := newDissemReg(t, "A") // member/leader
	svcB, dissemB := newDissemReg(t, "B") // non-member: empty engine + gossip cache

	p := mkPID("A", "1")
	if _, err := svcA.Register(context.Background(), "svc", p); err != nil {
		t.Fatalf("register: %v", err)
	}
	e, err := svcA.engine.Get(activeKey("svc"))
	if err != nil {
		t.Fatalf("read active: %v", err)
	}
	svcA.translateActive("svc", e.Value, 5, false)
	pump(dissemA, dissemB)

	res, err := svcB.Lookup(context.Background(), "svc")
	if err != nil || !res.Found || res.PID.String() != p.String() {
		t.Fatalf("non-member lookup via dissem: res=%+v err=%v", res, err)
	}
	if r, _ := svcB.Lookup(context.Background(), "absent"); r.Found {
		t.Fatalf("ungossiped name must not resolve on a non-member")
	}

	// Tombstone (delete at a higher raft index) gossips through and wins.
	svcA.translateActive("svc", nil, 6, true)
	pump(dissemA, dissemB)
	if r, _ := svcB.Lookup(context.Background(), "svc"); r.Found {
		t.Fatalf("name must stop resolving after a tombstone gossips through")
	}
}

// TestDissem_ReconcilerBroadcastsOnRegister proves the kv-watch reconciler feeds
// the dissem plane on a Consistent register without any manual translation.
func TestDissem_ReconcilerBroadcastsOnRegister(t *testing.T) {
	svcA, dissemA := newDissemReg(t, "A")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svcA.StartReconciler(ctx); err != nil {
		t.Fatalf("start reconciler: %v", err)
	}

	p := mkPID("A", "1")
	if _, err := svcA.RegisterScope(ctx, "svc", p, globalapi.Consistent); err != nil {
		t.Fatalf("register: %v", err)
	}
	if !eventually(t, 2*time.Second, func() bool { _, ok := dissemA.Lookup("svc"); return ok }) {
		t.Fatalf("reconciler must broadcast the active binding into dissem")
	}
}

// TestDissem_ReconcilerSeedsExistingActive proves a restarted registry service
// primes the dissem cache from the current kv snapshot before watching future
// changes. Without this, non-member/client lookups can miss stable names until
// another write happens.
func TestDissem_ReconcilerSeedsExistingActive(t *testing.T) {
	eng := systemkv.NewService("A", eventbus.NewBus(), nil)
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })

	p := mkPID("A", "1")
	writer := NewService(eng, "A", nil, nil)
	if _, err := writer.RegisterScope(context.Background(), "svc", p, globalapi.Consistent); err != nil {
		t.Fatalf("seed register: %v", err)
	}

	dissem := global.NewDissem("A", zap.NewNop())
	restarted := NewService(eng, "A", nil, nil)
	restarted.ConfigureDissem(dissem)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := restarted.StartReconciler(ctx); err != nil {
		t.Fatalf("start reconciler: %v", err)
	}

	got, ok := dissem.Lookup("svc")
	if !ok || got.String() != p.String() {
		t.Fatalf("reconciler seed must prime dissem cache: got=%s ok=%v", got, ok)
	}
}
