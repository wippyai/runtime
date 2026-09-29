// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
	sysprocess "github.com/wippyai/runtime/system/process"
)

// completingOnCancelProcess models a busy service root: termination arrives
// while its current step is still running, and its cleanup returns normally.
type completingOnCancelProcess struct {
	ctx     context.Context
	entered chan struct{}
	release chan struct{}
}

func (p *completingOnCancelProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.ctx = ctx
	return nil
}

func (p *completingOnCancelProcess) Step(_ []process.Event, out *process.StepOutput) error {
	close(p.entered)
	<-p.ctx.Done()
	<-p.release
	out.Done(nil)
	return nil
}

func (*completingOnCancelProcess) Close() {}

func TestTerminateDuringStepReportsTermination(t *testing.T) {
	results := make(chan *runtime.Result, 1)
	lc := &testLifecycle{onComplete: func(_ context.Context, _ pid.PID, result *runtime.Result) {
		results <- result
	}}
	sched := newTestSchedulerWithLifecycle(1, lc)
	sched.Start()
	defer testStopScheduler(sched)

	root := &completingOnCancelProcess{entered: make(chan struct{}), release: make(chan struct{})}
	rootPID := pid.PID{UniqID: "busy-service-root"}
	_, err := sched.Submit(context.Background(), rootPID, root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-root.entered:
	case <-time.After(time.Second):
		t.Fatal("root did not enter its startup step")
	}
	if err := sched.Terminate(rootPID); err != nil {
		t.Fatal(err)
	}
	close(root.release)
	select {
	case result := <-results:
		if !errors.Is(result.Error, sysprocess.ErrTerminated) {
			t.Fatalf("supervised root completed normally after termination: %v", result.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("terminated root did not complete")
	}
}
