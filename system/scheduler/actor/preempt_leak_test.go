// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	apiruntime "github.com/wippyai/runtime/api/runtime"
)

// preemptNProcess reports StepPreempted n times, then completes.
type preemptNProcess struct {
	closed *atomic.Int64
	left   int
}

func (*preemptNProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (p *preemptNProcess) Step(_ []process.Event, out *process.StepOutput) error {
	if p.left == 0 {
		out.Done(nil)
		return nil
	}
	p.left--
	out.Preempt()
	return nil
}
func (*preemptNProcess) Send(*relay.Package) error { return nil }
func (p *preemptNProcess) Close()                  { p.closed.Add(1) }

func settledGoroutines(t *testing.T, want int) int {
	t.Helper()
	var n int
	for i := 0; i < 200; i++ {
		runtime.GC()
		n = runtime.NumGoroutine()
		if n <= want {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
	return n
}

func TestPreemptCyclesReleaseEveryProcessAndGoroutine(t *testing.T) {
	baseline := settledGoroutines(t, 0)

	var completed, closed atomic.Int64
	lc := &testLifecycle{
		onComplete: func(context.Context, pidapi.PID, *apiruntime.Result) { completed.Add(1) },
	}
	sched := newPreemptTestScheduler(4, lc)
	sched.Start()

	const total = 3000
	for i := 0; i < total; i++ {
		p := &preemptNProcess{closed: &closed, left: i % 7}
		id := pidapi.PID{UniqID: fmt.Sprintf("p%d", i)}
		if _, err := sched.Submit(context.Background(), id, p, "", nil); err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.Now().Add(20 * time.Second)
	for completed.Load() < total && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := completed.Load(); got != total {
		t.Fatalf("completed %d of %d", got, total)
	}
	deadline = time.Now().Add(5 * time.Second)
	for closed.Load() < total && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := closed.Load(); got != total {
		t.Fatalf("closed %d of %d processes", got, total)
	}

	// Idle workers are the only parked ones, each counted once.
	deadline = time.Now().Add(5 * time.Second)
	for sched.parked.Load() != 4 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := sched.parked.Load(); got != 4 {
		t.Fatalf("expected 4 parked workers while idle, got %d", got)
	}

	testStopScheduler(sched)
	if got := sched.parked.Load(); got != 0 {
		t.Fatalf("parked counter drifted to %d after stop", got)
	}
	if got := settledGoroutines(t, baseline); got > baseline {
		t.Fatalf("goroutines grew from %d to %d", baseline, got)
	}
}

// Processes stopped while still preempting are closed and unregistered.
func TestStopClosesPreemptingProcesses(t *testing.T) {
	var closed atomic.Int64
	sched := newPreemptTestScheduler(2, &testLifecycle{})
	sched.Start()
	const total = 20
	for i := 0; i < total; i++ {
		p := &preemptNProcess{closed: &closed, left: 1 << 30}
		if _, err := sched.Submit(context.Background(), pidapi.PID{UniqID: fmt.Sprintf("s%d", i)}, p, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(50 * time.Millisecond)
	testStopScheduler(sched)
	if got := closed.Load(); got != total {
		t.Fatalf("closed %d of %d processes at stop", got, total)
	}
	if got := sched.parked.Load(); got != 0 {
		t.Fatalf("parked counter is %d after stop", got)
	}
}
