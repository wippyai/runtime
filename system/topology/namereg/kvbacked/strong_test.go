// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/eventbus"
	systemkv "github.com/wippyai/runtime/system/kv"
)

func newStrongReg(t *testing.T, members []pid.NodeID, deadline time.Duration, lc func(string, pid.PID) (pid.PID, bool)) *Service {
	t.Helper()
	eng := systemkv.NewService("reg", eventbus.NewBus(), nil)
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })
	r := NewService(eng, "node-1", nil, nil)
	r.ConfigureStrong(StrongDeps{
		Membership:    func() []pid.NodeID { return members },
		IsLeader:      func() bool { return true },
		LocalConflict: lc,
		Deadline:      deadline,
	})
	return r
}

func TestStrong_RegisterPromotes(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, 2*time.Second, nil)
	p := mkPID("node-1", "a")

	out, err := r.RegisterScope(context.Background(), "svc", p, globalapi.Strong)
	if err != nil {
		t.Fatalf("strong register: %v", err)
	}
	if out.State != globalapi.RegisterStateActive || out.PID.String() != p.String() {
		t.Fatalf("outcome: %+v", out)
	}

	res, _ := r.Lookup(context.Background(), "svc")
	if !res.Found || res.PID.String() != p.String() {
		t.Fatalf("lookup after strong promote: %+v", res)
	}
	if rp, ok := r.IsStrongReserved("svc"); !ok || rp.String() != p.String() {
		t.Fatalf("IsStrongReserved = %v,%v", rp, ok)
	}
}

func TestStrong_TimeoutWhenAckMissing(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, 300*time.Millisecond, nil)
	p := mkPID("node-1", "a")

	_, err := r.RegisterScope(context.Background(), "svc", p, globalapi.Strong)
	var te *globalapi.StrongRegistrationTimeoutError
	if !errors.As(err, &te) {
		t.Fatalf("want StrongRegistrationTimeoutError, got %v", err)
	}
	if res, _ := r.Lookup(context.Background(), "svc"); res.Found {
		t.Fatalf("name must not be active after timeout")
	}
}

func TestStrong_RejectOnLocalConflict(t *testing.T) {
	other := mkPID("node-1", "other")
	lc := func(string, pid.PID) (pid.PID, bool) { return other, true }
	r := newStrongReg(t, []pid.NodeID{"node-1"}, 2*time.Second, lc)
	p := mkPID("node-1", "a")

	_, err := r.RegisterScope(context.Background(), "svc", p, globalapi.Strong)
	var ce *globalapi.StrongConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("want StrongConflictError, got %v", err)
	}
	if res, _ := r.Lookup(context.Background(), "svc"); res.Found {
		t.Fatalf("rejected name must not be active")
	}
}

func TestStrong_ReservedDuringWindowThenReleased(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, 600*time.Millisecond, nil)
	p := mkPID("node-1", "a")

	done := make(chan error, 1)
	go func() {
		_, err := r.RegisterScope(context.Background(), "svc", p, globalapi.Strong)
		done <- err
	}()

	if !eventually(t, 2*time.Second, func() bool { _, ok := r.IsStrongReserved("svc"); return ok }) {
		t.Fatalf("name must be reserved during the promotion window")
	}

	if err := <-done; err == nil {
		t.Fatalf("register must expire when an ack is missing")
	}
	if !eventually(t, 2*time.Second, func() bool { _, ok := r.IsStrongReserved("svc"); return !ok }) {
		t.Fatalf("reservation must clear after expiry")
	}
}

func TestStrong_UnregisterClearsPending(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, 5*time.Second, nil)
	p := mkPID("node-1", "a")

	done := make(chan error, 1)
	go func() {
		_, err := r.RegisterScope(context.Background(), "svc", p, globalapi.Strong)
		done <- err
	}()
	if !eventually(t, 2*time.Second, func() bool { _, ok := r.IsStrongReserved("svc"); return ok }) {
		t.Fatalf("pending reservation expected")
	}

	if _, err := r.UnregisterScope(context.Background(), "svc", globalapi.Strong); err != nil {
		t.Fatalf("unregister strong: %v", err)
	}
	if err := <-done; err == nil {
		t.Fatalf("register must terminate after unregister")
	}
	if _, ok := r.IsStrongReserved("svc"); ok {
		t.Fatalf("reservation must be cleared after unregister")
	}
}

// TestStrong_RecoversActiveExclusionOnSeed proves a node restart re-latches the
// exclusion for an already-active Strong name during seed(), so IsStrongReserved
// stays correct after recovery (cross-scope guard not bypassed).
func TestStrong_RecoversActiveExclusionOnSeed(t *testing.T) {
	eng := systemkv.NewService("reg", eventbus.NewBus(), nil)
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })
	deps := StrongDeps{
		Membership: func() []pid.NodeID { return []pid.NodeID{"node-1"} },
		IsLeader:   func() bool { return true },
		Deadline:   2 * time.Second,
	}
	p := mkPID("node-1", "a")

	r1 := NewService(eng, "node-1", nil, nil)
	r1.ConfigureStrong(deps)
	if out, err := r1.RegisterScope(context.Background(), "svc", p, globalapi.Strong); err != nil || out.State != globalapi.RegisterStateActive {
		t.Fatalf("strong register: out=%+v err=%v", out, err)
	}

	// "Restart": fresh Service over the same engine; in-memory exclusions empty.
	r2 := NewService(eng, "node-1", nil, nil)
	r2.ConfigureStrong(deps)
	if _, ok := r2.IsStrongReserved("svc"); ok {
		t.Fatalf("exclusion must be empty before seed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := r2.StartReconciler(ctx); err != nil {
		t.Fatalf("start reconciler: %v", err)
	}
	// seed() runs synchronously in StartReconciler; the active Strong name must be
	// re-latched.
	if rp, ok := r2.IsStrongReserved("svc"); !ok || rp.String() != p.String() {
		t.Fatalf("active Strong exclusion not recovered on seed: %v,%v", rp, ok)
	}
}

// TestCrossScope_ConsistentBlockedByStrongPending proves a CONSISTENT register
// is refused (ErrPendingConflict) while a STRONG reservation for the same name
// is in flight — the cross-scope invariant that a pending owns the name.
func TestCrossScope_ConsistentBlockedByStrongPending(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, 5*time.Second, nil)
	p := mkPID("node-1", "a")
	go func() { _, _ = r.RegisterScope(context.Background(), "svc", p, globalapi.Strong) }()

	if !eventually(t, 2*time.Second, func() bool { _, ok := r.IsStrongReserved("svc"); return ok }) {
		t.Fatalf("strong pending expected")
	}
	_, err := r.RegisterScope(context.Background(), "svc", mkPID("node-1", "b"), globalapi.Consistent)
	if !errors.Is(err, globalapi.ErrPendingConflict) {
		t.Fatalf("CONSISTENT register during STRONG pending must be ErrPendingConflict, got %v", err)
	}
}

// TestCrossScope_ConsistentCannotDisplaceStrongActive proves a CONSISTENT
// register cannot take over a name already held by a STRONG owner, even with a
// custom resolver that would award the name to the incoming claimant.
func TestCrossScope_ConsistentCannotDisplaceStrongActive(t *testing.T) {
	eng := systemkv.NewService("reg", eventbus.NewBus(), nil)
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })
	// Resolver that always awards the name to the incoming claimant.
	r := NewService(eng, "node-1", func(_ string, _, incoming pid.PID) pid.PID { return incoming }, nil)
	r.ConfigureStrong(StrongDeps{
		Membership: func() []pid.NodeID { return []pid.NodeID{"node-1"} },
		IsLeader:   func() bool { return true },
		Deadline:   2 * time.Second,
	})
	strongPID := mkPID("node-1", "strong")
	if out, err := r.RegisterScope(context.Background(), "svc", strongPID, globalapi.Strong); err != nil || out.State != globalapi.RegisterStateActive {
		t.Fatalf("strong register: out=%+v err=%v", out, err)
	}

	_, err := r.RegisterScope(context.Background(), "svc", mkPID("node-2", "cons"), globalapi.Consistent)
	if !errors.Is(err, globalapi.ErrNameAlreadyRegistered) {
		t.Fatalf("CONSISTENT must not displace STRONG, got %v", err)
	}
	if res, _ := r.Lookup(context.Background(), "svc"); res.PID.String() != strongPID.String() {
		t.Fatalf("STRONG owner displaced: %s", res.PID)
	}
}

func eventually(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}
