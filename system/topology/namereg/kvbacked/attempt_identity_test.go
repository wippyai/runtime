// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

func TestStrongStartupRejectsMissingAttemptIdentity(t *testing.T) {
	for _, prefix := range []string{pendingPrefix, activePrefix} {
		t.Run(prefix, func(t *testing.T) {
			r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
			owner := mkPID("node-1", "owner")
			value, err := encode(map[string]any{"p": owner.String(), "n": "claim", "s": true, "r": []string{"node-1"}})
			if err != nil {
				t.Fatal(err)
			}
			key := prefix + "claim"
			if _, err := r.engine.Set(key, value); err != nil {
				t.Fatal(err)
			}
			err = r.StartReconciler(t.Context())
			if err == nil || !strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "missing Strong attempt identity") {
				t.Fatalf("expected record-specific format error, got %v", err)
			}
			if r.ready.Load() {
				t.Fatal("unsupported record opened admission")
			}
		})
	}
}

func TestStrongPendingDeleteAfterPromotionRetainsActiveRecord(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	startStrongReconciler(t, r)
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
	if err := r.transitionPendingDelete("claim", active.AttemptID, e.Epoch); err != nil {
		t.Fatal(err)
	}
	if got, ok := r.IsStrongReserved("claim"); !ok || !got.Equal(owner) {
		t.Fatalf("pending delete released promoted claim: %v, %v", got, ok)
	}
}

func TestStrongSameOwnerOldAttemptCannotCompleteReplacement(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	owner := mkPID("node-1", "owner")
	old := &strongWaiter{ch: make(chan strongCompletion, 1), attemptID: "old"}
	next := &strongWaiter{ch: make(chan strongCompletion, 1), attemptID: "next"}
	r.strong.addWaiter("claim", old)
	r.strong.addWaiter("claim", next)
	r.strong.onActive("claim", "old", 1, 1, owner)
	r.strong.onTerminal("claim", "old", 1)
	select {
	case out := <-old.ch:
		if out.out.State != globalapi.RegisterStateActive {
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
	r.strong.onActive("claim", "next", 3, 3, owner)
	select {
	case out := <-next.ch:
		if out.out.State != globalapi.RegisterStateActive || out.out.Epoch != 3 {
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

func TestStrongAttemptWaiterRemovalStopsDelivery(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	w := &strongWaiter{ch: make(chan strongCompletion, 1), attemptID: "canceled"}
	r.strong.addWaiter("claim", w)
	r.strong.removeWaiter("claim", w)
	r.strong.deliver("claim", "canceled", strongCompletion{out: globalapi.RegisterOutcome{State: globalapi.RegisterStateExpired}})
	select {
	case <-w.ch:
		t.Fatal("canceled attempt received a terminal result")
	default:
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
	// Discovery loss no longer rewrites participant sets. An explicit rewrite
	// still preserves the stored identity.
	hdr.RequiredNodes = []pid.NodeID{"node-1"}
	data, err = encode(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(pendingKey("claim"), data); err != nil {
		t.Fatal(err)
	}
	e, err := r.engine.Get(pendingKey("claim"))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := decodePending(e.Value)
	if err != nil || updated.AttemptID != "stable" || len(updated.RequiredNodes) != 1 {
		t.Fatalf("pending rewrite changed attempt: %+v, %v", updated, err)
	}
}
