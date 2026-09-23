// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/topology"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

func TestStrongLeaderCapturesCohortForFollowerRequest(t *testing.T) {
	leader := newStrongReg(t, []pid.NodeID{"node-1", "node-2", "node-3"}, 2*time.Second, nil)
	startStrongReconciler(t, leader)
	follower := NewService(leader.engine, "node-2", nil, nil)
	follower.ConfigureStrong(StrongDeps{
		IsLeader: func() bool { return false },
		Members: func() ([]pid.NodeID, error) {
			t.Error("follower's membership view must not define the observer cohort")
			return []pid.NodeID{"node-2"}, nil
		},
		Deadline: 2 * time.Second,
	})
	startStrongReconciler(t, follower)
	owner := mkPID("node-2", "owner")
	done := make(chan error, 1)
	go func() {
		_, err := follower.RegisterScope(t.Context(), "cohort", owner, globalapi.Strong)
		done <- err
	}()
	if !eventually(t, time.Second, func() bool {
		entry, err := leader.engine.Get(pendingKey("cohort"))
		if err != nil {
			return false
		}
		header, err := decodePending(entry.Value)
		return err == nil && reflect.DeepEqual(header.RequiredNodes, []pid.NodeID{"node-1", "node-2", "node-3"})
	}) {
		t.Fatal("leader did not stamp its complete observer configuration")
	}
	if _, err := leader.engine.Get(activeKey("cohort")); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("pending promoted before node-3 observed it: %v", err)
	}
	// The missing observer seeds the already pending record and ACKs it. No
	// naming enrollment or weak-scope exclusion is needed to join observation.
	third := NewService(leader.engine, "node-3", nil, nil)
	third.ConfigureStrong(StrongDeps{IsLeader: func() bool { return false }})
	startStrongReconciler(t, third)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("complete cohort did not promote: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("registration did not complete after final observer joined")
	}
}

func TestStrongDepartedObserverDoesNotPoisonNextAttempt(t *testing.T) {
	var members atomic.Value
	members.Store([]pid.NodeID{"node-1", "departed"})
	r := newStrongReg(t, nil, 150*time.Millisecond, nil)
	r.strong.members = func() ([]pid.NodeID, error) { return members.Load().([]pid.NodeID), nil }
	startStrongReconciler(t, r)
	owner := mkPID("node-1", "owner")
	done := make(chan error, 1)
	go func() {
		_, err := r.RegisterScope(t.Context(), "retry", owner, globalapi.Strong)
		done <- err
	}()
	if !eventually(t, time.Second, func() bool {
		entry, err := r.engine.Get(pendingKey("retry"))
		if err != nil {
			return false
		}
		header, err := decodePending(entry.Value)
		return err == nil && contains(header.RequiredNodes, "departed")
	}) {
		t.Fatal("first attempt never captured the missing observer")
	}
	// Removal changes only the next attempt. The existing one still owes the
	// acknowledgements it promised when its cohort was captured.
	members.Store([]pid.NodeID{"node-1"})
	select {
	case err := <-done:
		var timeout *globalapi.StrongRegistrationTimeoutError
		if !errors.As(err, &timeout) {
			t.Fatalf("captured attempt should time out, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("captured attempt did not reach its bounded outcome")
	}
	out, err := r.RegisterScope(t.Context(), "retry", owner, globalapi.Strong)
	if err != nil || out.State != globalapi.RegisterStateActive {
		t.Fatalf("removed observer permanently blocked the next attempt: %+v %v", out, err)
	}
	entry, err := r.engine.Get(activeKey("retry"))
	if err != nil {
		t.Fatal(err)
	}
	active, err := decodeActive(entry.Value)
	if err != nil || !reflect.DeepEqual(active.RequiredNodes, []pid.NodeID{"node-1"}) {
		t.Fatalf("second attempt used old membership: %+v %v", active, err)
	}
}

func TestStrongObserverConfigurationFailuresDoNotAssumeSingleton(t *testing.T) {
	for _, tc := range []struct {
		name    string
		members func() ([]pid.NodeID, error)
	}{
		{name: "unconfigured"},
		{name: "empty", members: func() ([]pid.NodeID, error) { return nil, nil }},
		{name: "failed", members: func() ([]pid.NodeID, error) { return nil, errors.New("configuration unavailable") }},
		{name: "missing_leader", members: func() ([]pid.NodeID, error) { return []pid.NodeID{"other"}, nil }},
		{name: "empty_identity", members: func() ([]pid.NodeID, error) { return []pid.NodeID{"node-1", ""}, nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newStrongReg(t, nil, time.Second, nil)
			r.strong.members = tc.members
			owner := mkPID("node-1", "owner")
			header := pendingHeader{Name: "invalid-cohort", PID: owner.String(), AttemptID: "unstamped"}
			before := putStrongPending(t, r.engine, header)
			if committed, err := r.strong.assignObservers(header.Name, before.Version, header); committed || err == nil {
				t.Fatalf("invalid configuration became a cohort: committed=%v err=%v", committed, err)
			}
			after, err := r.engine.Get(before.Key)
			if err != nil || after.Version != before.Version {
				t.Fatalf("failed configuration changed pending: %+v %v", after, err)
			}
		})
	}
}

func TestStrongCohortStampCannotRewriteCapturedOrReplacedAttempt(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "peer"}, time.Second, nil)
	owner := mkPID("node-1", "owner")
	header := pendingHeader{Name: "cohort-race", PID: owner.String(), AttemptID: "original"}
	before := putStrongPending(t, r.engine, header)
	replacement := header
	replacement.AttemptID = "replacement"
	after := putStrongPending(t, r.engine, replacement)
	if committed, err := r.strong.assignObservers(header.Name, before.Version, header); err != nil || committed {
		t.Fatalf("stale cohort stamp changed replacement: %v %v", committed, err)
	}
	if committed, err := r.strong.assignObservers(replacement.Name, after.Version, replacement); err != nil || !committed {
		t.Fatalf("current cohort stamp failed: %v %v", committed, err)
	}
	current, err := r.engine.Get(after.Key)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := decodePending(current.Value)
	if err != nil {
		t.Fatal(err)
	}
	r.strong.members = testStrongMembers
	if committed, err := r.strong.assignObservers(captured.Name, current.Version, captured); err != nil || committed {
		t.Fatalf("captured cohort was weakened after membership change: %v %v", committed, err)
	}
	if committed, err := r.strong.leaderPromote(header.Name, after.Epoch, after.Version, replacement); committed || err == nil {
		t.Fatalf("unstamped header promoted without observers: %v %v", committed, err)
	}
}

func TestStrongCoexistsWithLocalAndEventualBindings(t *testing.T) {
	r := newStrongReg(t, nil, time.Second, nil)
	startStrongReconciler(t, r)
	local := topology.NewPIDRegistry(topology.WithGlobalRegistry(r))
	eventualRegistry := eventual.NewService(eventual.Config{LocalNodeID: "node-1"})
	local.SetEventualRegistry(eventualRegistry)
	localOwner := mkPID("node-1", "local")
	eventualOwner := mkPID("node-1", "eventual")
	strongOwner := mkPID("node-1", "strong")
	if _, err := local.Register("shared", localOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := eventualRegistry.Register("shared", eventualOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RegisterScope(t.Context(), "shared", strongOwner, globalapi.Strong); err != nil {
		t.Fatalf("weaker bindings vetoed Strong observation: %v", err)
	}
	if _, err := local.Register("shared", localOwner); err != nil {
		t.Fatalf("Strong vetoed LOCAL registration: %v", err)
	}
	if _, err := eventualRegistry.Register("shared", eventualOwner); err != nil {
		t.Fatalf("Strong vetoed EVENTUAL registration: %v", err)
	}
	if got, ok := local.LookupLocal("shared"); !ok || !got.Equal(localOwner) {
		t.Fatalf("Strong changed the LOCAL binding: %v %v", got, ok)
	}
	if got, err := eventualRegistry.Lookup(context.Background(), "shared"); err != nil || !got.Found || !got.PID.Equal(eventualOwner) {
		t.Fatalf("Strong changed the EVENTUAL binding: %+v %v", got, err)
	}
	if got, ok, err := local.LookupContext(t.Context(), "shared"); err != nil || !ok || !got.Equal(strongOwner) {
		t.Fatalf("composed lookup lost global precedence: %v %v %v", got, ok, err)
	}
}
