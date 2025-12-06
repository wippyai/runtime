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

// TagTrackingCmd is a command that tracks Tag propagation.
type TagTrackingCmd struct {
	ID    int
	Value string
}

func (TagTrackingCmd) CmdID() dispatcher.CommandID { return 200 }

// TagTrackingHandler completes with the original command data.
type TagTrackingHandler struct{}

func (h *TagTrackingHandler) Handle(ctx context.Context, cmd dispatcher.Command, tag any, receiver dispatcher.ResultReceiver) error {
	c := cmd.(TagTrackingCmd)
	go receiver.CompleteYield(tag, c.Value, nil)
	return nil
}

// SingleYieldTagProcess yields one command at a time and verifies Tag is returned.
// This tests that the single-yield path in the actor scheduler properly propagates Tag.
type SingleYieldTagProcess struct {
	mu          sync.Mutex
	step        int
	receivedTag any
	ctx         context.Context
}

func (p *SingleYieldTagProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	p.ctx = ctx
	return nil
}

func (p *SingleYieldTagProcess) Step(events []Event, out *StepOutput) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Step 0: yield a command with a specific tag
	if p.step == 0 {
		p.step++
		out.Yield(TagTrackingCmd{ID: 1, Value: "test-value"}, "my-tag")
		out.Continue()
		return nil
	}

	// Step 1: verify we got results with the correct tag
	var data any
	for _, ev := range events {
		if ev.Type == EventYieldComplete {
			p.receivedTag = ev.Tag
			data = ev.Data
		}
	}

	out.Yield(CompleteCmd{Value: map[string]any{
		"receivedTag": p.receivedTag,
		"data":        data,
	}}, nil)
	out.Done(nil)
	return nil
}

func (p *SingleYieldTagProcess) Send(pkg *relay.Package) error { return nil }
func (p *SingleYieldTagProcess) Close()                        {}

// TestSingleYieldTagPropagation verifies that the single-yield path
// correctly propagates the Tag back to the process.
//
// This is a regression test for a bug where the single-yield optimization
// in worker.go used Processor.Complete() directly, which didn't set Tag,
// causing processes that relied on Tag for correlation to deadlock.
func TestSingleYieldTagPropagation(t *testing.T) {
	registry := NewRegistry()
	registry.Register(200, &TagTrackingHandler{})
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool
	var result *runtime.Result

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, res *runtime.Result) {
			result = res
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop()

	proc := &SingleYieldTagProcess{}
	_, err := sched.Submit(context.Background(), testPID(), proc, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("process did not complete - likely Tag was not propagated (deadlock)")
	}

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	// Verify the process received the Tag
	proc.mu.Lock()
	receivedTag := proc.receivedTag
	proc.mu.Unlock()

	if receivedTag != "my-tag" {
		t.Errorf("expected Tag 'my-tag', got %v", receivedTag)
	}
}

// SequentialTagYieldProcess yields one command per Step across multiple Steps.
// This simulates a process with sequential async operations (like the Lua Process
// with multiple coroutines each doing time.sleep one at a time).
type SequentialTagYieldProcess struct {
	mu           sync.Mutex
	step         int
	pendingTasks map[any]int // tag -> task id
	results      []int       // collected results
	ctx          context.Context
}

func (p *SequentialTagYieldProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	p.ctx = ctx
	p.pendingTasks = make(map[any]int)
	return nil
}

func (p *SequentialTagYieldProcess) Step(events []Event, out *StepOutput) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Handle incoming results first
	for _, ev := range events {
		if ev.Type == EventYieldComplete && ev.Tag != nil {
			if taskID, ok := p.pendingTasks[ev.Tag]; ok {
				p.results = append(p.results, taskID)
				delete(p.pendingTasks, ev.Tag)
			}
		}
	}

	// Yield new commands until we've done 3
	if p.step < 3 {
		p.step++
		tag := p.step // use step number as tag
		p.pendingTasks[tag] = p.step

		out.Yield(TagTrackingCmd{ID: p.step, Value: "value"}, tag)
		out.Continue()
		return nil
	}

	// Wait for all pending tasks
	if len(p.pendingTasks) > 0 {
		out.Continue()
		return nil
	}

	// All done
	out.Yield(CompleteCmd{Value: p.results}, nil)
	out.Done(nil)
	return nil
}

func (p *SequentialTagYieldProcess) Send(pkg *relay.Package) error { return nil }
func (p *SequentialTagYieldProcess) Close()                        {}

// TestSequentialYieldTagCorrelation verifies that when a process yields
// one command per Step across multiple Steps, each result is correctly
// correlated via Tag.
func TestSequentialYieldTagCorrelation(t *testing.T) {
	registry := NewRegistry()
	registry.Register(200, &TagTrackingHandler{})
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool
	var result *runtime.Result

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, res *runtime.Result) {
			result = res
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop()

	proc := &SequentialTagYieldProcess{}
	_, err := sched.Submit(context.Background(), testPID(), proc, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("process did not complete - Tag correlation likely broken")
	}

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	// Verify all 3 results were collected
	proc.mu.Lock()
	results := append([]int{}, proc.results...)
	proc.mu.Unlock()

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d: %v", len(results), results)
	}
}

// AsyncTagTrackingHandler completes asynchronously after a delay.
type AsyncTagTrackingHandler struct {
	delay time.Duration
}

func (h *AsyncTagTrackingHandler) Handle(ctx context.Context, cmd dispatcher.Command, tag any, receiver dispatcher.ResultReceiver) error {
	c := cmd.(TagTrackingCmd)
	go func() {
		select {
		case <-time.After(h.delay):
			receiver.CompleteYield(tag, c.Value, nil)
		case <-ctx.Done():
		}
	}()
	return nil
}

// StaggeredMultiYieldProcess simulates the Lua distributed_work scenario:
// - Process starts with N coroutines yielding simultaneously (multi-yield)
// - As each completes, that coroutine yields AGAIN
// - This creates "staggered" yields during an active multi-yield session
//
// The key issue: when one coroutine's yield completes and that coroutine
// immediately yields again, the scheduler must handle this new yield
// while still tracking other pending completions from the original batch.
type StaggeredMultiYieldProcess struct {
	mu            sync.Mutex
	pendingYields map[any]bool // tag -> true if pending
	completedTags []any        // tags that completed
	yieldCount    int          // total yields issued
	maxYields     int          // per-worker yields
	workers       int          // number of concurrent workers
	ctx           context.Context
}

func (p *StaggeredMultiYieldProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	p.ctx = ctx
	p.pendingYields = make(map[any]bool)
	p.workers = 3
	p.maxYields = 2 // each worker yields twice
	return nil
}

func (p *StaggeredMultiYieldProcess) Step(events []Event, out *StepOutput) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Process incoming results
	var lastTag any
	for _, ev := range events {
		if ev.Type == EventYieldComplete && ev.Tag != nil {
			delete(p.pendingYields, ev.Tag)
			p.completedTags = append(p.completedTags, ev.Tag)
			lastTag = ev.Tag
		}
	}

	// Initial step: yield for all workers simultaneously
	if p.yieldCount == 0 {
		for i := 0; i < p.workers; i++ {
			tag := i + 1 // tags 1, 2, 3
			p.pendingYields[tag] = true
			p.yieldCount++
			out.Yield(TagTrackingCmd{ID: tag, Value: "initial"}, tag)
		}
		out.Continue()
		return nil
	}

	// Count how many yields each original worker has done
	workerYields := make(map[int]int)
	for _, tag := range p.completedTags {
		if t, ok := tag.(int); ok {
			workerID := ((t - 1) % p.workers) + 1
			workerYields[workerID]++
		}
	}

	// For the just-completed result, if that worker needs more yields, issue one
	if lastTag != nil {
		if t, ok := lastTag.(int); ok {
			workerID := ((t - 1) % p.workers) + 1
			if workerYields[workerID] < p.maxYields {
				// This worker needs another yield
				newTag := p.yieldCount + 1
				p.pendingYields[newTag] = true
				p.yieldCount++
				out.Yield(TagTrackingCmd{ID: newTag, Value: "subsequent"}, newTag)
			}
		}
	}

	// Check if done
	if len(p.pendingYields) == 0 && len(p.completedTags) >= p.workers*p.maxYields {
		out.Yield(CompleteCmd{Value: p.completedTags}, nil)
		out.Done(nil)
		return nil
	}

	out.Continue()
	return nil
}

func (p *StaggeredMultiYieldProcess) Send(pkg *relay.Package) error { return nil }
func (p *StaggeredMultiYieldProcess) Close()                        {}

// TestStaggeredMultiYield tests the scenario where multiple concurrent yields
// are active, and as each completes, the process yields again.
//
// This is a regression test for the distributed_work deadlock where:
// 1. Process yields 3 commands (for 3 workers)
// 2. Handler 1 completes, process yields a NEW command for worker 1's next step
// 3. The scheduler must track both the 2 pending original yields AND the new yield
// 4. Without proper handling, the new yield gets lost causing deadlock
func TestStaggeredMultiYield(t *testing.T) {
	registry := NewRegistry()
	registry.Register(200, &AsyncTagTrackingHandler{delay: 5 * time.Millisecond})
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool
	var result *runtime.Result

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, res *runtime.Result) {
			result = res
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop()

	proc := &StaggeredMultiYieldProcess{}
	_, err := sched.Submit(context.Background(), testPID(), proc, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		proc.mu.Lock()
		pending := len(proc.pendingYields)
		completed := len(proc.completedTags)
		yields := proc.yieldCount
		proc.mu.Unlock()
		t.Fatalf("process did not complete - pending=%d, completed=%d, totalYields=%d (deadlock in staggered multi-yield)",
			pending, completed, yields)
	}

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	// Verify all expected yields completed (3 workers * 2 yields each = 6)
	proc.mu.Lock()
	completedCount := len(proc.completedTags)
	proc.mu.Unlock()

	expected := 3 * 2
	if completedCount != expected {
		t.Errorf("expected %d completed tags, got %d", expected, completedCount)
	}
}

// TestAsyncSingleYieldTagPropagation verifies Tag propagation with async handlers.
// This is closer to real-world usage where handlers complete asynchronously.
func TestAsyncSingleYieldTagPropagation(t *testing.T) {
	registry := NewRegistry()
	registry.Register(200, &AsyncTagTrackingHandler{delay: 10 * time.Millisecond})
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool
	var result *runtime.Result

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, res *runtime.Result) {
			result = res
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop()

	proc := &SingleYieldTagProcess{}
	_, err := sched.Submit(context.Background(), testPID(), proc, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("process did not complete with async handler - Tag not propagated")
	}

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	proc.mu.Lock()
	receivedTag := proc.receivedTag
	proc.mu.Unlock()

	if receivedTag != "my-tag" {
		t.Errorf("expected Tag 'my-tag', got %v", receivedTag)
	}
}
