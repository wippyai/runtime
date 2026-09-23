// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/topology"
	"github.com/wippyai/runtime/system/topology/namereg/admission"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

type clientReadProbe struct{ calls atomic.Int32 }

func (p *clientReadProbe) GetViaLeader(string) (kvapi.Entry, error) {
	p.calls.Add(1)
	return kvapi.Entry{}, kvapi.ErrKeyNotFound
}

type clientScopeProbe struct {
	registry *Service
	calls    atomic.Int32
}

func (p *clientScopeProbe) LookupOther(string, pid.PID) (pid.PID, bool, error) {
	p.calls.Add(1)
	return pid.PID{}, false, nil
}
func (p *clientScopeProbe) NameReady() bool { return p.registry.NameReady() }

func TestNonMemberCannotAdmitNamesByForwardingActiveMiss(t *testing.T) {
	gate := &admission.Coordinator{}
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Minute, nil)
	r.SetNonMember(func() bool { return true })
	remote := &clientReadProbe{}
	r.leaderRead = remote
	local := topology.NewPIDRegistry(topology.WithAdmissionCoordinator(gate))
	owner := mkPID("node-1", "owner")
	if _, err := local.Register("already-owned", owner); err != nil {
		t.Fatal(err)
	}
	local.SetGlobalRegistry(r)
	probe := &clientScopeProbe{registry: r}
	eventualReg := eventual.NewService(eventual.Config{
		LocalNodeID: "node-1", Admission: gate, CrossScope: probe,
	})
	if r.NameReady() {
		t.Fatal("client without a pending-name feed opened admission")
	}
	if _, err := local.Register("fresh-local", owner); !errors.Is(err, topapi.ErrNameServiceNotReady) {
		t.Fatalf("LOCAL admission without authoritative pending feed: %v", err)
	}
	if _, err := eventualReg.Register("fresh-eventual", owner); !errors.Is(err, eventual.ErrNameServiceNotReady) {
		t.Fatalf("EVENTUAL admission without authoritative pending feed: %v", err)
	}
	if remote.calls.Load() != 0 || probe.calls.Load() != 0 {
		t.Fatalf("admission forwarded while holding a name gate: remote=%d scope=%d", remote.calls.Load(), probe.calls.Load())
	}
	if got, err := local.Register("already-owned", owner); err != nil || !got.Equal(owner) {
		t.Fatalf("idempotent local owner cannot re-register: %v %v", got, err)
	}
	if remote.calls.Load() != 0 {
		t.Fatal("idempotent local registration forwarded a client lookup")
	}
	if result, err := r.Lookup(context.Background(), "ordinary-resolution"); err != nil || result.Found {
		t.Fatalf("ordinary resolution lost forwarding: %+v %v", result, err)
	}
	if remote.calls.Load() != 1 {
		t.Fatal("ordinary client lookup did not forward")
	}
}
