// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/eventbus"
	systemkv "github.com/wippyai/runtime/system/kv"
	local "github.com/wippyai/runtime/system/topology"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

func TestSeedRetainsPendingExclusionWithoutNewAcknowledgement(t *testing.T) {
	for _, mode := range []string{"joining-node", "previous-ack"} {
		t.Run(mode, func(t *testing.T) {
			engine := systemkv.NewService("reg", eventbus.NewBus(), nil)
			if _, err := engine.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer engine.Stop(context.Background())
			owner := mkPID("owner-node", "owner")
			required := []pid.NodeID{"owner-node"}
			if mode == "previous-ack" {
				required = append(required, "joining")
			}
			header, err := encode(pendingHeader{Name: "name", PID: owner.String(), NodeID: owner.Node, RequiredNodes: required})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := engine.Set(pendingKey("name"), header); err != nil {
				t.Fatal(err)
			}
			entry, err := engine.Get(pendingKey("name"))
			if err != nil {
				t.Fatal(err)
			}
			if mode == "previous-ack" {
				if _, err := engine.Set(ackKey("name", entry.Epoch, "joining"), []byte("joining")); err != nil {
					t.Fatal(err)
				}
			}
			r := NewService(engine, "joining", nil, nil)
			r.ConfigureStrong(StrongDeps{Membership: func() []pid.NodeID { return []pid.NodeID{"owner-node", "joining"} }, IsLeader: func() bool { return false }})
			if err := r.seed(); err != nil {
				t.Fatal(err)
			}
			got, held := r.IsStrongReserved("name")
			if !held || !got.Equal(owner) {
				t.Fatalf("pending exclusion missing: held=%v owner=%v", held, got)
			}
			_, ackErr := engine.Get(ackKey("name", entry.Epoch, "joining"))
			if mode == "joining-node" && ackErr == nil {
				t.Fatal("joining nonparticipant must not create an acknowledgement")
			}
		})
	}
}

type seedLocalRevoker struct {
	local    *local.PIDRegistry
	eventual *eventual.Service
}

func (r seedLocalRevoker) RevokeLocal(name string, keep pid.PID) bool {
	held, found := r.local.LookupLocal(name)
	if !found || held.Equal(keep) {
		return false
	}
	return r.local.Unregister(name)
}
func (r seedLocalRevoker) RevokeEventual(name string, keep pid.PID) bool {
	return r.eventual.RevokeForStrong(name, keep)
}

func TestSeedWithdrawsOnlyConflictingWeakerOwners(t *testing.T) {
	for _, stage := range []string{"pending", "active"} {
		for _, sameOwner := range []bool{false, true} {
			label := stage + "/conflict"
			if sameOwner {
				label = stage + "/same-owner"
			}
			t.Run(label, func(t *testing.T) {
				engine := systemkv.NewService("reg", eventbus.NewBus(), nil)
				if _, err := engine.Start(context.Background()); err != nil {
					t.Fatal(err)
				}
				defer engine.Stop(context.Background())
				guard := &topapi.NameGuard{}
				lr := local.NewPIDRegistry(local.WithNameGuard(guard))
				er := eventual.NewService(eventual.Config{LocalNodeID: "joining", NameGuard: guard})
				weak := mkPID("joining", "weak")
				owner := mkPID("owner-node", "owner")
				if sameOwner {
					owner = weak.Precomputed()
				}
				if _, err := lr.Register("local", weak); err != nil {
					t.Fatal(err)
				}
				if _, err := er.Register("eventual", weak); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"local", "eventual"} {
					var value []byte
					var err error
					key := pendingKey(name)
					if stage == "pending" {
						value, err = encode(pendingHeader{Name: name, PID: owner.String(), NodeID: owner.Node, RequiredNodes: []pid.NodeID{"owner-node"}})
					} else {
						key = activeKey(name)
						value, err = encode(activeValue{Name: name, PID: owner.String(), Strong: true})
					}
					if err != nil {
						t.Fatal(err)
					}
					if _, err := engine.Set(key, value); err != nil {
						t.Fatal(err)
					}
				}
				r := NewService(engine, "joining", nil, nil)
				r.ConfigureStrong(StrongDeps{NameGuard: guard, LocalRevoker: seedLocalRevoker{lr, er}, Membership: func() []pid.NodeID { return []pid.NodeID{"owner-node", "joining"} }, IsLeader: func() bool { return false }})
				if err := r.seed(); err != nil {
					t.Fatal(err)
				}
				_, localFound := lr.LookupLocal("local")
				eventualResult, err := er.Lookup(context.Background(), "eventual")
				if err != nil {
					t.Fatal(err)
				}
				if localFound != sameOwner || eventualResult.Found != sameOwner {
					t.Fatalf("same-owner=%v local=%v eventual=%v", sameOwner, localFound, eventualResult.Found)
				}
				for _, name := range []string{"local", "eventual"} {
					got, held := r.IsStrongReserved(name)
					if !held || !got.Equal(owner) {
						t.Fatalf("%s: missing exclusion", name)
					}
				}
			})
		}
	}
}

func TestLearnedExclusionDoesNotRegressOnStaleReplay(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, 0, nil)
	newer, older := mkPID("node-1", "new"), mkPID("node-1", "old")
	if ok, err := r.strong.learnExclusion("name", newer, 2, exclusionActive); !ok || err != nil {
		t.Fatal("new claim refused")
	}
	if ok, err := r.strong.learnExclusion("name", older, 1, exclusionPending); ok || err != nil {
		t.Fatal("stale claim accepted")
	}
	if ok, err := r.strong.learnExclusion("name", newer.Precomputed(), 2, exclusionPending); !ok || err != nil {
		t.Fatal("same-owner replay refused")
	}
	r.strong.mu.Lock()
	exclusion := r.strong.exclusions["name"]
	r.strong.mu.Unlock()
	if !exclusion.pid.Equal(newer) || exclusion.epoch != 2 || exclusion.state != exclusionActive {
		t.Fatal("active claim regressed")
	}
}

func TestStartupRefusesUnwithdrawnLocalConflict(t *testing.T) {
	owner, conflict := mkPID("owner-node", "owner"), mkPID("node-1", "conflict")
	r := newStrongReg(t, []pid.NodeID{"node-1"}, 0, func(string, pid.PID) (pid.PID, bool) { return conflict, true })
	value, err := encode(activeValue{Name: "name", PID: owner.String(), Strong: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(activeKey("name"), value); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := r.StartReconciler(ctx); err == nil {
		t.Fatal("startup succeeded without a way to withdraw the conflicting local name")
	}
	if r.NameReady() {
		t.Fatal("failed startup admitted names")
	}
}

type admissionSeedEngine struct {
	kvapi.Engine
	entered chan struct{}
	once    sync.Once
}

func (e *admissionSeedEngine) Scan(prefix string, fn func(kvapi.Entry) bool) error {
	return e.Engine.Scan(prefix, func(entry kvapi.Entry) bool {
		if prefix == activePrefix {
			e.once.Do(func() { close(e.entered) })
		}
		return fn(entry)
	})
}

func TestStartupCancellationInterruptsAdmissionWait(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, 0, nil)
	owner := mkPID("node-1", "owner")
	value, err := encode(activeValue{Name: "name", PID: owner.String(), Strong: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(activeKey("name"), value); err != nil {
		t.Fatal(err)
	}
	guard := &topapi.NameGuard{}
	r.strong.nameGuard = guard
	release, err := guard.LockContext(context.Background(), "name")
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	unlock := func() { once.Do(release) }
	defer unlock()
	engine := &admissionSeedEngine{Engine: r.engine, entered: make(chan struct{})}
	r.engine = engine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.StartReconciler(ctx) }()
	select {
	case <-engine.entered:
	case <-time.After(time.Second):
		unlock()
		cancel()
		<-done
		t.Fatal("startup never reached active scan")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("startup error=%v", err)
		}
	case <-time.After(time.Second):
		unlock()
		<-done
		t.Fatal("canceled startup remained blocked on name admission")
	}
	if r.NameReady() {
		t.Fatal("canceled startup became ready")
	}
}

type failingActiveRead struct {
	kvapi.Engine
	failure error
}

func (e failingActiveRead) Get(key string) (kvapi.Entry, error) {
	if key == activeKey("name") {
		return kvapi.Entry{}, e.failure
	}
	return e.Engine.Get(key)
}

func TestReconcileFailurePreservesExclusionAndClosesAdmission(t *testing.T) {
	for _, mode := range []string{"read-error", "malformed-record"} {
		t.Run(mode, func(t *testing.T) {
			r := newStrongReg(t, []pid.NodeID{"node-1"}, 0, nil)
			owner := mkPID("node-1", "owner")
			value, err := encode(activeValue{Name: "name", PID: owner.String(), Strong: true})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.engine.Set(activeKey("name"), value); err != nil {
				t.Fatal(err)
			}
			if err := r.strong.reconcile("name"); err != nil {
				t.Fatal(err)
			}
			r.ready.Store(true)
			if mode == "read-error" {
				r.engine = failingActiveRead{r.engine, errors.New("injected unavailable read")}
			} else {
				if _, err := r.engine.Set(activeKey("name"), []byte{0xc1}); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.strong.reconcile("name"); err == nil {
				t.Fatal("invalid authoritative read accepted")
			}
			if r.NameReady() {
				t.Fatal("reconciliation failure left admission open")
			}
			got, held := r.IsStrongReserved("name")
			if !held || !got.Equal(owner) {
				t.Fatal("failed read removed an existing exclusion")
			}
		})
	}
}
