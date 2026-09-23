// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	systemkv "github.com/wippyai/runtime/system/kv"
)

func newStrongReg(t *testing.T, members []pid.NodeID, deadline time.Duration, lc func(string, pid.PID) (pid.PID, bool)) *Service {
	t.Helper()
	eng := systemkv.NewService("reg", nil)
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })
	r := NewService(eng, "node-1", nil, nil)
	r.ConfigureStrong(StrongDeps{
		IsLeader: func() bool { return true },
		Members:  func() ([]pid.NodeID, error) { return append([]pid.NodeID{"node-1"}, members...), nil },
		Deadline: deadline,
	})
	return r
}

func startStrongReconciler(t *testing.T, r *Service) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := r.StartReconciler(ctx); err != nil {
		t.Fatalf("start reconciler: %v", err)
	}
}

func TestStrong_RegisterPromotes(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, 2*time.Second, nil)
	startStrongReconciler(t, r)
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
	startStrongReconciler(t, r)
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

func TestStrong_ReservedDuringWindowThenReleased(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, 600*time.Millisecond, nil)
	startStrongReconciler(t, r)
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
	startStrongReconciler(t, r)
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
	if !eventually(t, 2*time.Second, func() bool { _, ok := r.IsStrongReserved("svc"); return !ok }) {
		t.Fatalf("reservation must be cleared after unregister")
	}
}

// The Strong owner is a committed record, independent of reconciler lifetime.
func TestStrong_ActiveOwnerSurvivesReconcilerRestart(t *testing.T) {
	eng := systemkv.NewService("reg", nil)
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })
	deps := StrongDeps{Members: testStrongMembers,
		IsLeader: func() bool { return true },
		Deadline: 2 * time.Second,
	}
	p := mkPID("node-1", "a")

	r1 := NewService(eng, "node-1", nil, nil)
	r1.ConfigureStrong(deps)
	startStrongReconciler(t, r1)
	if out, err := r1.RegisterScope(context.Background(), "svc", p, globalapi.Strong); err != nil || out.State != globalapi.RegisterStateActive {
		t.Fatalf("strong register: out=%+v err=%v", out, err)
	}

	// A fresh Service reads the already committed owner before startup.
	r2 := NewService(eng, "node-1", nil, nil)
	r2.ConfigureStrong(deps)
	if got, ok := r2.IsStrongReserved("svc"); !ok || !got.Equal(p) {
		t.Fatalf("committed owner disappeared before seed: %v %v", got, ok)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := r2.StartReconciler(ctx); err != nil {
		t.Fatalf("start reconciler: %v", err)
	}
	// Starting the observer does not alter the committed owner.
	if rp, ok := r2.IsStrongReserved("svc"); !ok || rp.String() != p.String() {
		t.Fatalf("active Strong owner changed on seed: %v,%v", rp, ok)
	}
}

// TestGlobalModes_ConsistentBlockedByStrongPending proves a CONSISTENT register
// is refused (ErrPendingConflict) while a STRONG reservation for the same name
// is in flight — the shared-global-namespace invariant that a pending owns the name.
func TestGlobalModes_ConsistentBlockedByStrongPending(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, 5*time.Second, nil)
	startStrongReconciler(t, r)
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

// TestGlobalModes_ConsistentCannotDisplaceStrongActive proves a CONSISTENT
// register cannot take over a name already held by a STRONG owner, even with a
// custom resolver that would award the name to the incoming claimant.
func TestGlobalModes_ConsistentCannotDisplaceStrongActive(t *testing.T) {
	eng := systemkv.NewService("reg", nil)
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })
	// Resolver that always awards the name to the incoming claimant.
	r := NewService(eng, "node-1", func(_ string, _, incoming pid.PID) pid.PID { return incoming }, nil)
	r.ConfigureStrong(StrongDeps{Members: testStrongMembers,
		IsLeader: func() bool { return true },
		Deadline: 2 * time.Second,
	})
	startStrongReconciler(t, r)
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

// TestStrong_FalseNodeLeftDoesNotDeleteActiveOwner proves that a false
// NodeLeft hint cannot remove an already-promoted Strong binding or rewrite a
// pending claim's RequiredNodes. Node-2 remains in the live membership, so the
// pending claim must remain blocked on node-2's acknowledgement.
func TestStrong_FalseNodeLeftDoesNotDeleteActiveOwner(t *testing.T) {
	eng := systemkv.NewService("reg", nil)
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })
	r := NewService(eng, "node-1", nil, nil)
	r.ConfigureStrong(StrongDeps{Members: testStrongMembers,
		IsLeader: func() bool { return true },
		Deadline: time.Second,
	})
	startStrongReconciler(t, r)
	p := mkPID("node-1", "owner")
	if out, err := r.RegisterScope(context.Background(), "svc", p, globalapi.Strong); err != nil || out.State != globalapi.RegisterStateActive {
		t.Fatalf("strong register: out=%+v err=%v", out, err)
	}

	// Node-2 joins the observer configuration before the next reservation.
	// Later membership changes cannot rewrite this attempt's captured cohort.
	r.strong.members = func() ([]pid.NodeID, error) { return []pid.NodeID{"node-1", "node-2"}, nil }
	claim := mkPID("node-1", "claim")
	done := make(chan error, 1)
	go func() {
		_, err := r.RegisterScope(context.Background(), "pending", claim, globalapi.Strong)
		done <- err
	}()
	if !eventually(t, 2*time.Second, func() bool { _, ok := r.IsStrongReserved("pending"); return ok }) {
		t.Fatal("pending reservation expected")
	}
	r.strong.reconcile("pending")
	pe, err := r.engine.Get(pendingKey("pending"))
	if err != nil {
		t.Fatalf("pending claim disappeared after false NodeLeft: %v", err)
	}
	hdr, err := decodePending(pe.Value)
	if err != nil || !contains(hdr.RequiredNodes, "node-2") {
		t.Fatalf("RequiredNodes changed after false NodeLeft: header=%+v err=%v", hdr, err)
	}
	if res, err := r.Lookup(context.Background(), "pending"); err != nil {
		t.Fatalf("pending lookup: %v", err)
	} else if res.Found {
		t.Fatalf("pending claim promoted without node-2 acknowledgement: %+v", res)
	}

	// The active Strong owner remains available while the pending claim waits.
	res, err := r.Lookup(context.Background(), "svc")
	if err != nil {
		t.Fatalf("lookup after false leave: %v", err)
	}
	if !res.Found || res.PID.String() != p.String() {
		t.Fatalf("active Strong owner was removed by discovery state: %+v", res)
	}
	err = <-done
	var te *globalapi.StrongRegistrationTimeoutError
	if !errors.As(err, &te) {
		t.Fatalf("pending claim did not fail closed: %v", err)
	}
}

// TestStrong_FailsClosedWhenRequiredNodeDeparts proves that a membership
// snapshot cannot rewrite a committed reservation's RequiredNodes. A pending
// reservation that needs node B still waits for B's acknowledgement after B
// leaves, and expires instead of promoting on the remaining acknowledgements.
func TestStrong_FailsClosedWhenRequiredNodeDeparts(t *testing.T) {
	eng := systemkv.NewService("reg", nil)
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })

	r := NewService(eng, "node-1", nil, nil)
	r.ConfigureStrong(StrongDeps{Members: testStrongMembers,
		IsLeader: func() bool { return true },
		Deadline: 300 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := r.StartReconciler(ctx); err != nil {
		t.Fatalf("start reconciler: %v", err)
	}
	r.strong.members = func() ([]pid.NodeID, error) { return []pid.NodeID{"node-1", "ghost"}, nil }

	p := mkPID("node-1", "a")
	done := make(chan globalapi.RegisterOutcome, 1)
	errc := make(chan error, 1)
	go func() {
		out, err := r.RegisterScope(context.Background(), "svc", p, globalapi.Strong)
		if err != nil {
			errc <- err
			return
		}
		done <- out
	}()

	// node-1 acks itself; "ghost" never will, so the reservation is pending.
	if !eventually(t, 3*time.Second, func() bool { _, ok := r.IsStrongReserved("svc"); return ok }) {
		t.Fatalf("pending reservation expected")
	}

	// "ghost" leaves the membership (gossip drop). The leader must retain it in
	// RequiredNodes and fail closed when its acknowledgement never arrives.

	select {
	case out := <-done:
		t.Fatalf("reservation promoted without node-2 acknowledgement: %+v", out)
	case err := <-errc:
		var te *globalapi.StrongRegistrationTimeoutError
		if !errors.As(err, &te) {
			t.Fatalf("want StrongRegistrationTimeoutError, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reservation neither failed closed nor timed out")
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

func testStrongMembers() ([]pid.NodeID, error) { return []pid.NodeID{"node-1"}, nil }
