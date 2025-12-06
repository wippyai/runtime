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

// These tests prove the issues with index-based correlation in multi-yield.
// The current design uses array index to correlate yield->result, which breaks
// when yields span multiple Steps (e.g., concurrent coroutines yielding at different times).

const (
	CmdTaggedSleep dispatcher.CommandID = 200
)

// TaggedSleepCmd carries an identifier so we can track which yield it came from
type TaggedSleepCmd struct {
	Tag      string
	Duration time.Duration
}

func (TaggedSleepCmd) CmdID() dispatcher.CommandID { return CmdTaggedSleep }

// TaggedSleepHandler completes after delay, returning the tag
type TaggedSleepHandler struct{}

func (h *TaggedSleepHandler) Handle(ctx context.Context, cmd dispatcher.Command, tag any, receiver dispatcher.ResultReceiver) error {
	c := cmd.(TaggedSleepCmd)
	go func() {
		select {
		case <-time.After(c.Duration):
			receiver.CompleteYield(tag, c.Tag, nil) // Return tag as result
		case <-ctx.Done():
		}
	}()
	return nil
}

// SequentialYieldProcess simulates two coroutines yielding at different times.
//
// Step 0: Yield slow command (tag="A", 100ms) and fast command (tag="B", 10ms)
// Step 1: B completes first, process records result
// Step 2: A completes, process records result
//
// Expected: Results should be ["B", "A"] (completion order)
// Bug: With index-based correlation, results may be ["A", "B"] or incorrectly mapped
type SequentialYieldProcess struct {
	step          int
	results       []string
	mu            sync.Mutex
	totalExpected int
	ctx           context.Context
}

func (p *SequentialYieldProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	p.ctx = ctx
	p.totalExpected = 2
	return nil
}

func (p *SequentialYieldProcess) Step(events []Event, out *StepOutput) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.step == 0 {
		// First step: yield both commands
		p.step++
		out.Yield(TaggedSleepCmd{Tag: "A", Duration: 100 * time.Millisecond}, "tag_A") // Slow
		out.Yield(TaggedSleepCmd{Tag: "B", Duration: 10 * time.Millisecond}, "tag_B")  // Fast
		out.Continue()
		return nil
	}

	// Record which result we got
	for _, ev := range events {
		if ev.Type == EventYieldComplete && ev.Data != nil {
			tag := ev.Data.(string)
			p.results = append(p.results, tag)
		}
	}

	// Check if we have all results
	if len(p.results) >= p.totalExpected {
		out.Done(payload.New(p.results))
		return nil
	}

	// Keep waiting
	out.Continue()
	return nil
}

func (p *SequentialYieldProcess) Send(pkg *relay.Package) error { return nil }
func (p *SequentialYieldProcess) Close()                        {}

// TestIndexCorrelationBreaks proves that index-based correlation delivers results
// in the wrong order when yields complete out of order.
//
// This test PASSES when the bug exists (showing the wrong behavior).
// After the fix, it should FAIL (because results will be in completion order).
func TestIndexCorrelationBreaks_ActorScheduler(t *testing.T) {
	registry := NewRegistry()
	registry.Register(CmdTaggedSleep, &TaggedSleepHandler{})
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, res *runtime.Result) {
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(4), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop()

	proc := &SequentialYieldProcess{}
	_, err := sched.Submit(context.Background(), testPID(), proc, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Wait for completion
	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("process did not complete")
	}

	proc.mu.Lock()
	results := append([]string{}, proc.results...)
	proc.mu.Unlock()

	t.Logf("Result order: %v", results)

	// CORRECT behavior: results should be ["B", "A"] (completion order)
	// BUG: results are ["A", "B"] (index order) or incorrect mapping
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// This assertion proves the bug exists if it passes
	// After fix, this should fail and below assertion should pass
	if results[0] == "A" && results[1] == "B" {
		t.Log("BUG CONFIRMED: Results are in index order, not completion order")
		t.Log("Expected: [B, A] (completion order)")
		t.Log("Got: [A, B] (index order - WRONG)")
	}

	// This is the correct expectation - should pass after fix
	if results[0] != "B" || results[1] != "A" {
		t.Errorf("Expected result order [B, A] (completion order), got %v", results)
	}
}

// StaggeredYieldProcess simulates a more complex scenario:
// - First step yields A (slow 200ms) and B (fast 10ms)
// - B completes, process does some work, then yields C (medium 50ms)
// - C completes while A is still pending
// - A finally completes
//
// With index-based correlation:
// - A has index 0, B has index 1
// - After B completes and we yield C, C gets index 0 (new step!)
// - When A completes with index 0, it gets correlated to C's slot
//
// This is the fundamental flaw in index-based correlation.
type StaggeredYieldProcess struct {
	step        int
	results     map[string]bool // Track which tags we received
	resultOrder []string        // Track order
	mu          sync.Mutex
	ctx         context.Context
}

func (p *StaggeredYieldProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	p.ctx = ctx
	p.results = make(map[string]bool)
	return nil
}

func (p *StaggeredYieldProcess) Step(events []Event, out *StepOutput) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Record results if present
	var gotB bool
	for _, ev := range events {
		if ev.Type == EventYieldComplete && ev.Data != nil {
			tag := ev.Data.(string)
			p.results[tag] = true
			p.resultOrder = append(p.resultOrder, tag)
			if tag == "B" {
				gotB = true
			}
		}
	}

	switch p.step {
	case 0:
		// Step 0: Yield A (slow) and B (fast)
		p.step++
		out.Yield(TaggedSleepCmd{Tag: "A", Duration: 200 * time.Millisecond}, "tag_A")
		out.Yield(TaggedSleepCmd{Tag: "B", Duration: 10 * time.Millisecond}, "tag_B")
		out.Continue()
		return nil

	case 1:
		// Step 1: B completed, now yield C (medium)
		// A is still pending!
		if gotB {
			p.step++
			out.Yield(TaggedSleepCmd{Tag: "C", Duration: 50 * time.Millisecond}, "tag_C")
			out.Continue()
			return nil
		}
		// Unexpected result in step 1
		out.Continue()
		return nil

	default:
		// Subsequent steps: wait for all results
		if len(p.results) >= 3 {
			out.Done(payload.New(p.resultOrder))
			return nil
		}
		out.Continue()
		return nil
	}
}

func (p *StaggeredYieldProcess) Send(pkg *relay.Package) error { return nil }
func (p *StaggeredYieldProcess) Close()                        {}

// TestStaggeredYields proves that index correlation breaks across steps.
//
// The scenario:
// 1. Yield A (index 0, slow) and B (index 1, fast)
// 2. B completes first, Step called with index 1
// 3. Process yields C (now index 0 in new context!)
// 4. C completes, Step called with index 0
// 5. A completes, Step called with index 0 <-- COLLISION with C's index!
//
// With proper tag-based correlation, each yield carries its identity.
func TestStaggeredYields_IndexCollision(t *testing.T) {
	registry := NewRegistry()
	registry.Register(CmdTaggedSleep, &TaggedSleepHandler{})
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, res *runtime.Result) {
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(4), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop()

	proc := &StaggeredYieldProcess{}
	_, err := sched.Submit(context.Background(), testPID(), proc, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("process did not complete")
	}

	proc.mu.Lock()
	results := proc.results
	order := append([]string{}, proc.resultOrder...)
	proc.mu.Unlock()

	t.Logf("Results received: %v", results)
	t.Logf("Order received: %v", order)

	// Verify we got all three results
	if !results["A"] || !results["B"] || !results["C"] {
		t.Errorf("Missing results: A=%v, B=%v, C=%v", results["A"], results["B"], results["C"])
	}

	// Expected order: B (10ms), C (50ms after B), A (200ms total)
	// With correct implementation: [B, C, A]
	expectedOrder := []string{"B", "C", "A"}
	if len(order) != 3 {
		t.Fatalf("Expected 3 results, got %d: %v", len(order), order)
	}

	for i, expected := range expectedOrder {
		if order[i] != expected {
			t.Errorf("Position %d: expected %s, got %s", i, expected, order[i])
		}
	}
}

// ParallelCoroutinesProcess simulates real-world usage:
// Multiple coroutines running concurrently, each yielding multiple times.
//
// Coroutine 1: yields X1, X2, X3 (fast, 5ms each)
// Coroutine 2: yields Y1 (slow, 100ms)
//
// X1, X2, X3 should all complete and be processed while Y1 is still pending.
// Each coroutine needs its results delivered correctly.
type ParallelCoroutinesProcess struct {
	step            int
	xResults        []string // Results for coroutine X
	yResults        []string // Results for coroutine Y
	yieldedXCount   int      // How many X yields we've done
	xYieldsExpected int
	mu              sync.Mutex
	ctx             context.Context
}

func (p *ParallelCoroutinesProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	p.ctx = ctx
	p.xYieldsExpected = 3
	return nil
}

func (p *ParallelCoroutinesProcess) Step(events []Event, out *StepOutput) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Record results
	for _, ev := range events {
		if ev.Type == EventYieldComplete && ev.Data != nil {
			tag := ev.Data.(string)
			if len(tag) > 0 && tag[0] == 'X' {
				p.xResults = append(p.xResults, tag)
			} else if len(tag) > 0 && tag[0] == 'Y' {
				p.yResults = append(p.yResults, tag)
			}
		}
	}

	// Check if done
	if len(p.xResults) >= p.xYieldsExpected && len(p.yResults) >= 1 {
		out.Done(payload.New(map[string][]string{
			"X": p.xResults,
			"Y": p.yResults,
		}))
		return nil
	}

	// Keep yielding X until we've done all
	if p.yieldedXCount < p.xYieldsExpected {
		p.yieldedXCount++
		// Yield next X (fast) and Y1 (slow, only on first iteration)
		tag := "X" + string(rune('0'+p.yieldedXCount))
		out.Yield(TaggedSleepCmd{Tag: tag, Duration: 5 * time.Millisecond}, "tag_"+tag)
		if p.yieldedXCount == 1 {
			out.Yield(TaggedSleepCmd{Tag: "Y1", Duration: 100 * time.Millisecond}, "tag_Y1")
		}
		out.Continue()
		return nil
	}

	// Just waiting for Y1 to complete
	out.Continue()
	return nil
}

func (p *ParallelCoroutinesProcess) Send(pkg *relay.Package) error { return nil }
func (p *ParallelCoroutinesProcess) Close()                        {}

// TestParallelCoroutines proves that concurrent coroutine patterns break with index correlation.
func TestParallelCoroutines_IndexCorrelation(t *testing.T) {
	registry := NewRegistry()
	registry.Register(CmdTaggedSleep, &TaggedSleepHandler{})
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, res *runtime.Result) {
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(4), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop()

	proc := &ParallelCoroutinesProcess{}
	_, err := sched.Submit(context.Background(), testPID(), proc, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("process did not complete")
	}

	proc.mu.Lock()
	xResults := append([]string{}, proc.xResults...)
	yResults := append([]string{}, proc.yResults...)
	proc.mu.Unlock()

	t.Logf("X results: %v", xResults)
	t.Logf("Y results: %v", yResults)

	// Verify X got all its results
	if len(xResults) != 3 {
		t.Errorf("Expected 3 X results, got %d: %v", len(xResults), xResults)
	}

	// Verify Y got its result
	if len(yResults) != 1 || (len(yResults) > 0 && yResults[0] != "Y1") {
		t.Errorf("Expected Y results [Y1], got %v", yResults)
	}

	// Verify no cross-contamination (X results in Y or vice versa)
	for _, r := range xResults {
		if len(r) > 0 && r[0] != 'X' {
			t.Errorf("X results contains non-X value: %s", r)
		}
	}
	for _, r := range yResults {
		if len(r) > 0 && r[0] != 'Y' {
			t.Errorf("Y results contains non-Y value: %s", r)
		}
	}
}
