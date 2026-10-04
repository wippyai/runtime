// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	secapi "github.com/wippyai/runtime/api/security"
)

// grantingIncarnation records the grants it holds on entry, acquires one more
// and upgrades to the next incarnation until none is left.
type grantingIncarnation struct {
	next    *process.UpgradeRequest
	acquire pidapi.PID
	seen    chan []bool
	check   []pidapi.PID
}

func (p *grantingIncarnation) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	grants := secapi.GetProcessSendGrants(ctx)
	held := make([]bool, len(p.check))
	for i, c := range p.check {
		held[i] = grants.Holds(c)
	}
	p.seen <- held
	grants.Grant(p.acquire)
	return nil
}

func (p *grantingIncarnation) Step(_ []process.Event, out *process.StepOutput) error {
	if p.next != nil {
		out.SetUpgrade(p.next)
		return nil
	}
	out.Done(nil)
	return nil
}

func (*grantingIncarnation) Send(*relay.Package) error { return nil }
func (*grantingIncarnation) Close()                    {}

func TestUpgradeChainKeepsSendGrantsAcquiredAfterEarlierUpgrades(t *testing.T) {
	a := pidapi.PID{UniqID: "a"}
	b := pidapi.PID{UniqID: "b"}
	done := make(chan struct{})
	sched := newTestSchedulerWithLifecycle(1, &testLifecycle{
		onComplete: func(ctx context.Context, _ pidapi.PID, _ *runtime.Result) {
			ctxapi.CompleteFrame(ctx)
			if fc := ctxapi.ExecutionFrame(ctx); fc != nil {
				_ = fc.Close()
			}
			close(done)
		},
	})
	sched.Start()
	defer testStopScheduler(sched)

	seen := make(chan []bool, 4)
	up := func(name string) *process.UpgradeRequest {
		return &process.UpgradeRequest{Source: registry.NewID("app", name)}
	}
	first := &grantingIncarnation{seen: seen, next: up("second")}
	incarnations := map[string]*grantingIncarnation{
		"second": {seen: seen, acquire: a, next: up("third")},
		"third":  {seen: seen, acquire: b, check: []pidapi.PID{a}, next: up("fourth")},
		"fourth": {seen: seen, check: []pidapi.PID{a, b}},
	}
	appCtx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	process.WithFactory(appCtx, &mockFactory{
		createFunc: func(id registry.ID) (process.Process, *process.Meta, error) {
			return incarnations[id.Name], &process.Meta{}, nil
		},
	})

	self := pidapi.PID{UniqID: "grantee"}
	rootCtx, fc := ctxapi.OpenFrameContext(appCtx)
	if err := fc.SetMultiple(
		ctxapi.Pair{Key: runtime.FrameIDKey, Value: registry.NewID("app", "first")},
		ctxapi.Pair{Key: runtime.FramePIDKey, Value: self},
		secapi.ProcessSendGrantsPair(secapi.NewProcessSendGrants()),
	); err != nil {
		t.Fatal(err)
	}
	fc.Seal()

	if _, err := sched.Submit(rootCtx, self, first, "", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not complete")
	}
	close(seen)

	var views [][]bool
	for v := range seen {
		views = append(views, v)
	}
	if len(views) != 4 {
		t.Fatalf("expected four incarnations to report, got %d", len(views))
	}
	if !views[2][0] {
		t.Error("the third incarnation holds the grant the second acquired")
	}
	if !views[3][0] || !views[3][1] {
		t.Errorf("the fourth incarnation holds grants from both earlier incarnations, got %v", views[3])
	}
}
