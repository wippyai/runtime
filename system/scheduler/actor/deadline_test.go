// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/runtime/wasm/engine"
	actorhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	"github.com/wippyai/runtime/system/scheduler"
)

type deadlineTestProcess struct {
	timeout     time.Duration
	initCtx     context.Context
	closed      atomic.Bool
	steps       atomic.Int32
	idleOnStep  bool
	yieldOnStep bool
}

func (p *deadlineTestProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.initCtx = ctx
	return nil
}

func (p *deadlineTestProcess) Step(_ []process.Event, out *process.StepOutput) error {
	p.steps.Add(1)
	if p.idleOnStep {
		out.Idle()
		return nil
	}
	if p.yieldOnStep {
		out.Yield(YieldCmd{}, 0)
		out.Continue()
		return nil
	}
	out.Done(nil)
	return nil
}

func (p *deadlineTestProcess) Close() {
	p.closed.Store(true)
}

func (p *deadlineTestProcess) ExecutionTimeout() time.Duration {
	return p.timeout
}

// TestDeadline_TighterParentDeadlineRespect verifies that if parent ctx has an earlier
// deadline than process ExecutionTimeout, the parent's earlier deadline is preserved.
func TestDeadline_TighterParentDeadlineRespect(t *testing.T) {
	registry := scheduler.NewRegistry()
	sched := NewScheduler(registry, WithWorkers(2))
	sched.Start()
	defer sched.Stop(context.Background())

	parentTimeout := 30 * time.Millisecond
	processTimeout := 500 * time.Millisecond

	parentCtx, cancel := context.WithTimeout(context.Background(), parentTimeout)
	defer cancel()

	proc := &deadlineTestProcess{timeout: processTimeout}
	pid := pidapi.PID{UniqID: "tighter-parent-1"}

	_, err := sched.Submit(parentCtx, pid, proc, "", nil)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	deadline, ok := proc.initCtx.Deadline()
	if !ok {
		t.Fatal("expected deadline to be set on processor context")
	}

	parentDeadline, _ := parentCtx.Deadline()
	if !deadline.Equal(parentDeadline) {
		t.Fatalf("expected processor deadline %v to match tighter parent deadline %v", deadline, parentDeadline)
	}
}

// TestDeadline_ProcessTimeoutEarlier verifies that if process ExecutionTimeout is earlier
// than parent ctx deadline (or parent has no deadline), the process timeout is applied.
func TestDeadline_ProcessTimeoutEarlier(t *testing.T) {
	registry := scheduler.NewRegistry()
	sched := NewScheduler(registry, WithWorkers(2))
	sched.Start()
	defer sched.Stop(context.Background())

	processTimeout := 40 * time.Millisecond
	parentTimeout := 5 * time.Second

	parentCtx, cancel := context.WithTimeout(context.Background(), parentTimeout)
	defer cancel()

	proc := &deadlineTestProcess{timeout: processTimeout}
	pid := pidapi.PID{UniqID: "earlier-process-1"}

	start := time.Now()
	_, err := sched.Submit(parentCtx, pid, proc, "", nil)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	deadline, ok := proc.initCtx.Deadline()
	if !ok {
		t.Fatal("expected deadline to be set on processor context")
	}

	expectedDeadline := start.Add(processTimeout)
	diff := deadline.Sub(expectedDeadline)
	if diff < -20*time.Millisecond || diff > 20*time.Millisecond {
		t.Fatalf("expected deadline near %v, got %v (diff %v)", expectedDeadline, deadline, diff)
	}
}

// TestDeadline_NegativeTimeoutFailsClosed verifies that a negative ExecutionTimeout
// is rejected before Init and fails closed with ErrInvalidExecutionTimeout.
func TestDeadline_NegativeTimeoutFailsClosed(t *testing.T) {
	registry := scheduler.NewRegistry()
	sched := NewScheduler(registry, WithWorkers(2))
	sched.Start()
	defer sched.Stop(context.Background())

	proc := &deadlineTestProcess{timeout: -10 * time.Millisecond}
	pid := pidapi.PID{UniqID: "neg-timeout-1"}

	_, err := sched.Submit(context.Background(), pid, proc, "", nil)
	if err == nil {
		t.Fatal("expected Submit to fail for negative timeout, but got nil")
	}
	if !errors.Is(err, process.ErrInvalidExecutionTimeout) {
		t.Fatalf("expected ErrInvalidExecutionTimeout, got: %v", err)
	}
	if proc.initCtx != nil {
		t.Fatal("expected process.Init not to be called on invalid timeout")
	}
}

// TestDeadline_IndefiniteProcessUnchanged verifies that timeout=0 or non-timeouter
// processes do not have a deadline set if parent context has none.
func TestDeadline_IndefiniteProcessUnchanged(t *testing.T) {
	registry := scheduler.NewRegistry()
	sched := NewScheduler(registry, WithWorkers(2))
	sched.Start()
	defer sched.Stop(context.Background())

	procZero := &deadlineTestProcess{timeout: 0}
	pidZero := pidapi.PID{UniqID: "zero-timeout-1"}

	_, err := sched.Submit(context.Background(), pidZero, procZero, "", nil)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}
	if _, ok := procZero.initCtx.Deadline(); ok {
		t.Fatal("expected no deadline for indefinite process (timeout=0)")
	}

	// Plain process without ExecutionTimeoutProvider interface
	plainProc := &ImmediateProcess{}
	pidPlain := pidapi.PID{UniqID: "plain-process-1"}
	_, err = sched.Submit(context.Background(), pidPlain, plainProc, "", nil)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}
}

// TestDeadline_ActorProcessTimeoutMethod tests that ActorProcess returns the configured
// MaxExecutionMS lifetime as a time.Duration, 0 for indefinite, and passes negative through.
func TestDeadline_ActorProcessTimeoutMethod(t *testing.T) {
	// Nil process returns 0
	nilActor := engine.NewActorProcess(nil, actorhost.Limits{}, nil)
	if nilActor.ExecutionTimeout() != 0 {
		t.Fatalf("expected 0 for nil process, got %v", nilActor.ExecutionTimeout())
	}

	// Positive MaxExecutionMS
	p1 := engine.NewProcess(nil, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{MaxExecutionMS: 250}, nil)
	a1 := engine.NewActorProcess(p1, actorhost.Limits{}, nil)
	if a1.ExecutionTimeout() != 250*time.Millisecond {
		t.Fatalf("expected 250ms, got %v", a1.ExecutionTimeout())
	}

	// Zero MaxExecutionMS (indefinite)
	p2 := engine.NewProcess(nil, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{MaxExecutionMS: 0}, nil)
	a2 := engine.NewActorProcess(p2, actorhost.Limits{}, nil)
	if a2.ExecutionTimeout() != 0 {
		t.Fatalf("expected 0 (indefinite), got %v", a2.ExecutionTimeout())
	}

	// Negative MaxExecutionMS (fails closed via scheduler)
	p3 := engine.NewProcess(nil, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{MaxExecutionMS: -50}, nil)
	a3 := engine.NewActorProcess(p3, actorhost.Limits{}, nil)
	if a3.ExecutionTimeout() != -50*time.Millisecond {
		t.Fatalf("expected -50ms, got %v", a3.ExecutionTimeout())
	}
}

func TestDeadline_IdleExpiresWithoutMessages(t *testing.T) {
	sched := NewScheduler(scheduler.NewRegistry(), WithWorkers(1))
	sched.Start()
	defer sched.Stop(context.Background())
	p := &deadlineTestProcess{timeout: 30 * time.Millisecond, idleOnStep: true}
	if _, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "idle-deadline"}, p, "", nil); err != nil {
		t.Fatal(err)
	}
	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !p.closed.Load() {
		select {
		case <-timeout.C:
			t.Fatal("idle process did not expire without messages")
		case <-tick.C:
		}
	}
	if p.steps.Load() == 0 {
		t.Fatal("fixture never reached mailbox wait")
	}
}
