// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/scheduler"
)

// yieldingProcess yields tag 1 on its first step. Its second step either
// requests an upgrade or, when it has no request, records yield completions.
type yieldingProcess struct {
	upgrade   *process.UpgradeRequest
	completed chan any
	steps     int
}

func (*yieldingProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (p *yieldingProcess) Step(events []process.Event, out *process.StepOutput) error {
	p.steps++
	if p.steps == 1 {
		out.Yield(YieldCmd{}, 1)
		out.WaitForYields()
		return nil
	}
	for _, e := range events {
		if e.Type == process.EventYieldComplete {
			p.completed <- e.Data
			out.Done(nil)
			return nil
		}
	}
	if p.upgrade != nil && len(events) > 0 {
		out.SetUpgrade(p.upgrade)
		return nil
	}
	out.WaitForYields()
	return nil
}
func (*yieldingProcess) Send(*relay.Package) error { return nil }
func (*yieldingProcess) Close()                    {}

// A yield completion of the replaced code never reaches the replacement, even
// when its tag matches a yield of the replacement; the replacement's own
// completion is delivered.
func TestUpgradeRetiresYieldCompletionsOfReplacedCode(t *testing.T) {
	receivers := make(chan dispatcher.ResultReceiver, 2)
	reg := scheduler.NewRegistry()
	reg.Register(CmdYield, dispatcher.HandlerFunc(func(_ context.Context, _ dispatcher.Command, _ uint64, r dispatcher.ResultReceiver) error {
		receivers <- r
		return nil
	}))
	reg.Register(CmdComplete, CompleteHandler())
	sched := NewScheduler(reg, WithWorkers(1), WithLifecycle(&testLifecycle{}))
	sched.Start()
	defer testStopScheduler(sched)

	completed := make(chan any, 2)
	next := registry.NewID("app", "next")
	factory := &mockFactory{createFunc: func(registry.ID) (process.Process, *process.Meta, error) {
		return &yieldingProcess{completed: completed}, &process.Meta{Method: "main"}, nil
	}}
	appCtx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	process.WithFactory(appCtx, factory)
	frameCtx, fc := ctxapi.OpenFrameContext(appCtx)
	fc.Seal()
	id := pidapi.PID{UniqID: "yielder"}
	source := &yieldingProcess{upgrade: &process.UpgradeRequest{Source: next}, completed: completed}
	if _, err := sched.Submit(frameCtx, id, source, "", nil); err != nil {
		t.Fatal(err)
	}
	oldReceiver := receive(t, receivers, "source yield")
	if err := sched.Send(&relay.Package{Target: id}); err != nil {
		t.Fatal(err)
	}
	newReceiver := receive(t, receivers, "replacement yield")

	oldReceiver.CompleteYield(1, "stale", nil)
	newReceiver.CompleteYield(1, "fresh", nil)

	select {
	case got := <-completed:
		if got != "fresh" {
			t.Fatalf("replacement received %v, want its own completion", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("replacement did not receive its completion")
	}
}

func receive(t *testing.T, ch <-chan dispatcher.ResultReceiver, what string) dispatcher.ResultReceiver {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatalf("%s was not dispatched", what)
		return nil
	}
}
