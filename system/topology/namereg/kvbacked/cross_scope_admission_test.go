// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	topapi "github.com/wippyai/runtime/api/topology"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/topology"
	"github.com/wippyai/runtime/system/topology/namereg/admission"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

type otherScopeChecker struct {
	strong *Service
	local  *topology.PIDRegistry
}

type pausedGlobalLookup struct {
	topapi.GlobalRegistry
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *pausedGlobalLookup) Lookup(ctx context.Context, name string, opts ...globalapi.LookupOption) (globalapi.LookupResult, error) {
	result, err := g.GlobalRegistry.Lookup(ctx, name, opts...)
	if name == "race" {
		g.once.Do(func() { close(g.entered); <-g.release })
	}
	return result, err
}

func (c otherScopeChecker) LookupOther(name string, _ pid.PID) (pid.PID, bool, error) {
	if p, ok := c.strong.IsStrongReserved(name); ok {
		return p, true, nil
	}
	p, ok := c.local.LookupLocal(name)
	return p, ok, nil
}
func (otherScopeChecker) NameReady() bool { return true }

func TestStrongPendingVoteExcludesLocalAndEventualBeforeAckCommits(t *testing.T) {
	gate := &admission.Coordinator{}
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, time.Minute, nil)
	local := topology.NewPIDRegistry(topology.WithGlobalRegistry(r), topology.WithAdmissionCoordinator(gate))
	eventualReg := eventual.NewService(eventual.Config{LocalNodeID: "node-1", Admission: gate,
		CrossScope: otherScopeChecker{strong: r, local: local}})
	r.ConfigureStrong(StrongDeps{
		Admission: gate,
		IsLeader:  func() bool { return false },
		Deadline:  time.Minute,
		LocalConflict: func(name string, proposed pid.PID) (pid.PID, bool, error) {
			if p, ok := local.LookupLocal(name); ok {
				return p, true, nil
			}
			return eventualReg.ConflictingLiveClaim(name, proposed)
		},
	})
	const attempt = "00000000000000000000000000000088"
	held := &heldVoteEngine{Engine: r.engine, key: ackKey("held", attempt, "node-1"),
		entered: make(chan struct{}), release: make(chan struct{})}
	r.engine = held
	t.Cleanup(held.Release)
	startStrongReconciler(t, r)
	claimant := mkPID("node-1", "claimant")
	value, err := encode(pendingHeader{PID: claimant.String(), Name: "held", AttemptID: attempt,
		RequiredNodes: []pid.NodeID{"node-1", "ghost"}, DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := held.Set(pendingKey("held"), value); err != nil {
		t.Fatal(err)
	}
	select {
	case <-held.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Strong worker did not reach paused vote")
	}
	if _, err := held.Get(ackKey("held", attempt, "node-1")); err == nil {
		t.Fatal("vote already committed while held")
	}
	competitor := mkPID("node-1", "competitor")
	if _, err := local.Register("held", competitor); err == nil {
		t.Fatal("LOCAL admitted competitor after Strong prepared its ACK")
	}
	if _, err := eventualReg.Register("held", competitor); err == nil {
		t.Fatal("EVENTUAL admitted competitor after Strong prepared its ACK")
	}
	if _, err := local.Register("independent", competitor); err != nil {
		t.Fatalf("unrelated LOCAL name stalled behind pending vote: %v", err)
	}
	held.Release()
	if !eventually(t, 2*time.Second, func() bool {
		_, err := held.Get(ackKey("held", attempt, "node-1"))
		return err == nil
	}) {
		t.Fatal("admitted Strong vote did not complete")
	}
}

func TestStrongVoteRejectsHiddenEventualDot(t *testing.T) {
	gate := &admission.Coordinator{}
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, time.Minute, nil)
	a := eventual.NewService(eventual.Config{LocalNodeID: "node-1", Admission: gate})
	b := eventual.NewService(eventual.Config{LocalNodeID: "node-2"})
	hidden := mkPID("node-1", "hidden")
	claimant := mkPID("node-2", "winner")
	if _, err := a.Register("hidden", hidden); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RegisterWithOptions("hidden", claimant, eventual.WithPriority(1)); err != nil {
		t.Fatal(err)
	}
	for _, frame := range b.DrainBroadcasts(0, 1<<20) {
		a.OnFrame(frame)
	}
	if got, err := a.Lookup(context.Background(), "hidden"); err != nil || !got.Found || !got.PID.Equal(claimant) {
		t.Fatalf("fixture requires visible matching winner: %+v, err=%v", got, err)
	}
	r.ConfigureStrong(StrongDeps{
		Admission: gate,
		IsLeader:  func() bool { return false },
		Deadline:  time.Minute,
		LocalConflict: func(name string, proposed pid.PID) (pid.PID, bool, error) {
			return a.ConflictingLiveClaim(name, proposed)
		},
	})
	startStrongReconciler(t, r)
	const attempt = "00000000000000000000000000000090"
	value, err := encode(pendingHeader{PID: claimant.String(), Name: "hidden", AttemptID: attempt,
		RequiredNodes: []pid.NodeID{"node-1", "ghost"}, DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(pendingKey("hidden"), value); err != nil {
		t.Fatal(err)
	}
	if !eventually(t, 2*time.Second, func() bool {
		_, err := r.engine.Get(rejectKey("hidden", attempt, "node-1"))
		return err == nil
	}) {
		t.Fatal("Strong voter ACKed while hidden EVENTUAL dot remained live")
	}
	if _, err := r.engine.Get(ackKey("hidden", attempt, "node-1")); err == nil {
		t.Fatal("Strong voter wrote both ACK and rejection")
	}
}

func TestStrongVoteSeesLocalAdmissionThatStartedFirst(t *testing.T) {
	gate := &admission.Coordinator{}
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, time.Minute, nil)
	lookup := &pausedGlobalLookup{GlobalRegistry: r, entered: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-lookup.release:
		default:
			close(lookup.release)
		}
	})
	local := topology.NewPIDRegistry(topology.WithGlobalRegistry(lookup), topology.WithAdmissionCoordinator(gate))
	r.ConfigureStrong(StrongDeps{Admission: gate,
		IsLeader: func() bool { return false },
		Deadline: time.Minute,
		LocalConflict: func(name string, _ pid.PID) (pid.PID, bool, error) {
			p, ok := local.LookupLocal(name)
			return p, ok, nil
		},
	})
	startStrongReconciler(t, r)
	localPID := mkPID("node-1", "local-first")
	localDone := make(chan error, 1)
	go func() { _, err := local.Register("race", localPID); localDone <- err }()
	select {
	case <-lookup.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("LOCAL did not reach paused conflict check")
	}
	const attempt = "00000000000000000000000000000089"
	strongPID := mkPID("node-1", "strong-second")
	value, err := encode(pendingHeader{PID: strongPID.String(), Name: "race", AttemptID: attempt,
		RequiredNodes: []pid.NodeID{"node-1", "ghost"}, DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(pendingKey("race"), value); err != nil {
		t.Fatal(err)
	}
	close(lookup.release)
	if err := <-localDone; err != nil {
		t.Fatalf("LOCAL admitted first but failed: %v", err)
	}
	if !eventually(t, 2*time.Second, func() bool {
		_, err := r.engine.Get(rejectKey("race", attempt, "node-1"))
		return err == nil
	}) {
		t.Fatal("Strong ACK shadowed the earlier LOCAL admission")
	}
	if _, err := r.engine.Get(ackKey("race", attempt, "node-1")); err == nil {
		t.Fatal("Strong committed both ACK and reject for a conflicting LOCAL name")
	}
}
