// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

func TestStrongPendingDeleteAfterPromotionRetainsActiveExclusion(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	owner := mkPID("node-1", "owner")
	if _, err := r.RegisterScope(context.Background(), "claim", owner, globalapi.Strong); err != nil {
		t.Fatal(err)
	}
	e, err := r.engine.Get(activeKey("claim"))
	if err != nil {
		t.Fatal(err)
	}
	active, err := decodeActive(e.Value)
	if err != nil || active.AttemptID == "" {
		t.Fatalf("promoted attempt missing: %+v, %v", active, err)
	}
	if err := r.strong.reconcileDeleted("claim", active.AttemptID, true); err != nil {
		t.Fatal(err)
	}
	if got, ok := r.IsStrongReserved("claim"); !ok || !got.Equal(owner) {
		t.Fatalf("pending delete released promoted claim: %v, %v", got, ok)
	}
}

func TestStrongExistingActiveWithoutAttemptReleasesOnDelete(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	owner := mkPID("node-1", "owner")
	value, err := encode(activeValue{PID: owner.String(), Name: "claim", Strong: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(activeKey("claim"), value); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := r.StartReconciler(ctx); err != nil {
		t.Fatal(err)
	}
	if got, ok := r.IsStrongReserved("claim"); !ok || !got.Equal(owner) {
		t.Fatalf("existing active was not restored at startup: %v, %v", got, ok)
	}
	e, err := r.engine.Get(activeKey("claim"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.CompareAndDelete(activeKey("claim"), e.Version); err != nil {
		t.Fatal(err)
	}
	if err := r.strong.reconcile("claim"); err != nil {
		t.Fatal(err)
	}
	if got, ok := r.IsStrongReserved("claim"); ok {
		t.Fatalf("existing active exclusion was retained after deletion: %v", got)
	}
}

func TestStrongSameOwnerOldAttemptCannotCompleteReplacement(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	owner := mkPID("node-1", "owner")
	old := &strongWaiter{ch: make(chan globalapi.RegisterOutcome, 1), attemptID: "old", pid: owner}
	next := &strongWaiter{ch: make(chan globalapi.RegisterOutcome, 1), attemptID: "next", pid: owner}
	r.strong.addWaiter("claim", old)
	r.strong.addWaiter("claim", next)
	r.strong.latch("claim", owner, "next", 2)
	r.strong.onActive("claim", 1, "old", owner)
	r.strong.onTerminal("claim", "old")
	select {
	case out := <-old.ch:
		if out.State != globalapi.RegisterStateExpired {
			t.Fatalf("old attempt terminal outcome: %+v", out)
		}
	default:
		t.Fatal("old waiter was left waiting after replacement")
	}
	select {
	case out := <-next.ch:
		t.Fatalf("old attempt completed replacement: %+v", out)
	default:
	}
	r.strong.onActive("claim", 3, "next", owner)
	select {
	case out := <-next.ch:
		if out.State != globalapi.RegisterStateActive || out.Epoch != 3 {
			t.Fatalf("replacement outcome: %+v", out)
		}
	default:
		t.Fatal("replacement did not receive its promotion")
	}
	select {
	case out := <-old.ch:
		t.Fatalf("old attempt received replacement promotion: %+v", out)
	default:
	}
}

func TestStrongAttemptDetailsAreReleasedWhenWaiterLeaves(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	w := &strongWaiter{ch: make(chan globalapi.RegisterOutcome, 1), attemptID: "canceled", pid: mkPID("node-1", "owner")}
	r.strong.addWaiter("claim", w)
	r.strong.setTerminal("claim", "canceled", "deadline", nil, 5)
	r.strong.removeWaiter("claim", w)
	r.strong.setTerminal("claim", "canceled", "deadline", nil, 5)
	r.strong.mu.Lock()
	defer r.strong.mu.Unlock()
	if len(r.strong.terminalReason) != 0 || len(r.strong.terminalEpoch) != 0 || len(r.strong.terminalMissing) != 0 {
		t.Fatal("terminal details retained after canceled attempt")
	}
}

func TestStrongAttemptSurvivesPendingMembershipRewrite(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "peer"}, time.Second, nil)
	r.strong.isLeader = func() bool { return false }
	owner := mkPID("node-1", "owner")
	hdr := pendingHeader{PID: owner.String(), Name: "claim", AttemptID: "stable", RequiredNodes: []pid.NodeID{"node-1", "peer"}}
	data, err := encode(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(pendingKey("claim"), data); err != nil {
		t.Fatal(err)
	}
	r.strong.dropNodeFromPending("claim", "peer")
	e, err := r.engine.Get(pendingKey("claim"))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := decodePending(e.Value)
	if err != nil || updated.AttemptID != "stable" || len(updated.RequiredNodes) != 1 {
		t.Fatalf("pending rewrite changed attempt: %+v, %v", updated, err)
	}
}
