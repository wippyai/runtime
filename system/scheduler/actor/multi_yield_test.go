package actor

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

var _ = runtime.Result{} // ensure import used

// Test command IDs for multi-yield tests
const (
	CmdDelayedComplete dispatcher.CommandID = 100
)

// DelayedCompleteCmd completes after a specified delay
type DelayedCompleteCmd struct {
	ID    int
	Delay time.Duration
	Value any
}

func (DelayedCompleteCmd) CmdID() dispatcher.CommandID { return CmdDelayedComplete }

// DelayedHandler completes after delay, tracking completion order
type DelayedHandler struct {
	completionOrder *[]int
	mu              *sync.Mutex
}

func (h *DelayedHandler) Handle(ctx context.Context, cmd dispatcher.Command, tag any, receiver dispatcher.ResultReceiver) error {
	c := cmd.(DelayedCompleteCmd)
	go func() {
		select {
		case <-time.After(c.Delay):
			h.mu.Lock()
			*h.completionOrder = append(*h.completionOrder, c.ID)
			h.mu.Unlock()
			receiver.CompleteYield(tag, c.Value, nil)
		case <-ctx.Done():
		}
	}()
	return nil
}

// MultiYieldProcess yields multiple commands in one Step, tracks resume order
type MultiYieldProcess struct {
	yields      []DelayedCompleteCmd
	resumeOrder []int
	mu          sync.Mutex
	step        int
	ctx         context.Context
}

func (p *MultiYieldProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	p.ctx = ctx
	if len(input) > 0 {
		p.yields = input[0].Data().([]DelayedCompleteCmd)
	}
	return nil
}

func (p *MultiYieldProcess) Step(events []Event, out *StepOutput) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// First step: yield all commands with Tag = command ID
	if p.step == 0 {
		p.step++
		for _, cmd := range p.yields {
			out.Yield(cmd, cmd.ID) // Tag = command ID for correlation
		}
		out.Continue()
		return nil
	}

	// Subsequent steps: record which yield completed via Tag
	for _, ev := range events {
		if ev.Type == EventYieldComplete && ev.Tag != nil {
			if id, ok := ev.Tag.(int); ok {
				p.resumeOrder = append(p.resumeOrder, id)
			}
		}
	}

	// Check if we have results for all yields
	if len(p.resumeOrder) >= len(p.yields) {
		out.Yield(CompleteCmd{Value: p.resumeOrder}, nil)
		out.Done(nil)
		return nil
	}

	// Keep waiting for more
	out.Continue()
	return nil
}

func (p *MultiYieldProcess) Send(pkg *relay.Package) error { return nil }
func (p *MultiYieldProcess) Close()                        {}

// TestMultiYieldIndependentCompletion verifies that yields complete independently.
//
// Scenario: Process yields 2 commands
//   - Command 0: completes after 100ms
//   - Command 1: completes after 10ms
//
// Expected behavior (CORRECT):
//   - Command 1 completes first (10ms)
//   - Process is stepped with result for command 1
//   - Command 0 completes later (100ms)
//   - Process is stepped with result for command 0
//   - Resume order: [1, 0]
//
// Current behavior (BROKEN):
//   - Waits for BOTH commands (100ms total)
//   - Process gets both results at once
//   - Resume order: [0, 1] (in original order, not completion order)
func TestMultiYieldIndependentCompletion(t *testing.T) {
	var completionOrder []int
	var mu sync.Mutex

	handler := &DelayedHandler{
		completionOrder: &completionOrder,
		mu:              &mu,
	}

	registry := NewRegistry()
	registry.Register(CmdDelayedComplete, handler)
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, res *runtime.Result) {
			_ = res
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(2), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop()

	// Yield 2 commands: ID=0 slow (100ms), ID=1 fast (10ms)
	yields := []DelayedCompleteCmd{
		{ID: 0, Delay: 100 * time.Millisecond, Value: "slow"},
		{ID: 1, Delay: 10 * time.Millisecond, Value: "fast"},
	}

	proc := &MultiYieldProcess{}
	_, err := sched.Submit(context.Background(), testPID(), proc, "", testInput(yields))
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Wait for completion
	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("process did not complete")
	}

	// Check handler completion order - should be [1, 0] (fast first)
	mu.Lock()
	handlerOrder := append([]int{}, completionOrder...)
	mu.Unlock()

	t.Logf("Handler completion order: %v", handlerOrder)
	if len(handlerOrder) != 2 || handlerOrder[0] != 1 || handlerOrder[1] != 0 {
		t.Errorf("Expected handler completion order [1, 0], got %v", handlerOrder)
	}

	// Check process resume order
	proc.mu.Lock()
	resumeOrder := append([]int{}, proc.resumeOrder...)
	proc.mu.Unlock()

	t.Logf("Process resume order: %v", resumeOrder)

	// CORRECT behavior: resume order should match handler completion order [1, 0]
	// BROKEN behavior: resume order is [0, 1] because we wait for all and return in original order
	if len(resumeOrder) != 2 {
		t.Errorf("Expected 2 resumes, got %d", len(resumeOrder))
	}

	// This assertion will FAIL with current broken implementation
	// It passes after the fix is implemented
	if resumeOrder[0] != 1 || resumeOrder[1] != 0 {
		t.Errorf("Expected resume order [1, 0] (completion order), got %v (batched order)", resumeOrder)
	}
}

// TestMultiYieldProcessRunsWhileWaiting verifies process can do work while
// waiting for slow yields.
//
// Scenario: Process yields 2 commands
//   - Command 0: completes after 200ms
//   - Command 1: completes after 10ms
//   - After command 1 completes, process should be able to run more steps
//
// With CORRECT design: process steps 3 times (initial + 2 results)
// With BROKEN design: process only steps 2 times (initial + all results)
func TestMultiYieldProcessRunsWhileWaiting(t *testing.T) {
	var stepCount atomic.Int32

	// Custom process that counts steps
	type StepCountProcess struct {
		MultiYieldProcess
		stepCount *atomic.Int32
	}

	// Wrap Step to count
	origStep := func(p *MultiYieldProcess, events []Event, out *StepOutput) error {
		return p.Step(events, out)
	}
	_ = origStep // suppress unused warning

	// TODO: implement proper step counting when fix is ready
	t.Skip("Test skeleton - implement after multi-yield fix")

	_ = stepCount // suppress unused warning
}
