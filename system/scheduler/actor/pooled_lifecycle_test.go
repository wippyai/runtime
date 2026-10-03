// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	sysprocess "github.com/wippyai/runtime/system/process"
)

// idleForeverProcess waits for messages and never finishes on its own.
type idleForeverProcess struct{}

func (idleForeverProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (idleForeverProcess) Step(_ []process.Event, out *process.StepOutput) error {
	out.Idle()
	return nil
}
func (idleForeverProcess) Send(*relay.Package) error { return nil }
func (idleForeverProcess) Close()                    {}

func waitProcessorState(t *testing.T, proc *Processor, want ProcessState) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for ProcessState(proc.state.Load())&stateMask != want {
		if time.Now().After(deadline) {
			t.Fatalf("processor did not reach %s, is %s", StateName(want), StateName(ProcessState(proc.state.Load())))
		}
		time.Sleep(time.Millisecond)
	}
}

// Terminate ends a pooled execution that waits for messages: the owner
// scheduled it, so its result must arrive.
func TestTerminateEndsIdlePooledProcessor(t *testing.T) {
	sched := newTestSchedulerWithLifecycle(2, &testLifecycle{})
	sched.Start()
	defer testStopScheduler(sched)

	id := pidapi.PID{UniqID: "pooled-idle"}
	proc, err := sched.CreateProcessor(context.Background(), id, idleForeverProcess{})
	if err != nil {
		t.Fatal(err)
	}
	results := proc.resultCh
	sched.global.Push(proc)
	sched.wakeAny()
	waitProcessorState(t, proc, StateIdle)

	if err := sched.Terminate(id); err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-results:
		if !errors.Is(res.Error, sysprocess.ErrTerminated) {
			t.Fatalf("expected termination, got %v", res.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("terminated pooled execution never completed")
	}
	sched.ReleaseProcessor(proc)
}

// Stop returns as soon as the last pooled processor is released.
func TestStopReturnsWhenLastPooledProcessorIsReleased(t *testing.T) {
	sched := newTestSchedulerWithLifecycle(2, &testLifecycle{})
	sched.Start()

	proc, err := sched.CreateProcessor(context.Background(), pidapi.PID{UniqID: "pooled-held"}, idleForeverProcess{})
	if err != nil {
		t.Fatal(err)
	}

	stopped := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		sched.Stop(ctx)
		close(stopped)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !sched.isStopping() {
		if time.Now().After(deadline) {
			t.Fatal("stop did not begin")
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	sched.ReleaseProcessor(proc)

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop kept waiting after the last processor was released")
	}
}
