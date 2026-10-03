// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

// stepRecorder counts the steps it runs.
type stepRecorder struct{ steps atomic.Int32 }

func (*stepRecorder) Init(context.Context, string, payload.Payloads) error { return nil }

func (p *stepRecorder) Step(_ []process.Event, out *process.StepOutput) error {
	p.steps.Add(1)
	out.Done(nil)
	return nil
}

func (*stepRecorder) Send(*relay.Package) error { return nil }
func (*stepRecorder) Close()                    {}

// A pooled processor can still sit in a worker's queue from its previous
// incarnation. Until Submit has initialized it, popping that stale entry
// must not run the new process.
func TestSubmitPublishesProcessorOnlyWhenInitialized(t *testing.T) {
	proc := &stepRecorder{}
	self := pidapi.PID{UniqID: "half-initialized"}
	var sched *Scheduler
	var ranEarly int32
	sched = newTestSchedulerWithLifecycle(1, &testLifecycle{
		onStart: func(context.Context, pidapi.PID, process.Process) {
			v, ok := sched.byPID.Load(self.String())
			if !ok {
				t.Error("the processor is registered before lifecycle start")
				return
			}
			// A worker pops the stale entry while Submit is still running.
			sched.workerSnapshot()[0].executeOne(v.(*Processor))
			ranEarly = proc.steps.Load()
		},
	})
	sched.Start()
	defer testStopScheduler(sched)

	if _, err := sched.Submit(context.Background(), self, proc, "", nil); err != nil {
		t.Fatal(err)
	}
	if ranEarly != 0 {
		t.Fatalf("a stale queue entry ran the process before Submit initialized it (%d steps)", ranEarly)
	}
}
