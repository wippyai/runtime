package pool

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// These tests prove the issues with index-based correlation in pool multi-yield.
// Same issues as actor scheduler - index correlation breaks across Steps.

const (
	CmdTaggedSleep  dispatcher.CommandID = 200
	CmdPoolComplete dispatcher.CommandID = 201
)

// Yield correlation tags
const (
	tagA uint64 = 1
	tagB uint64 = 2
	tagC uint64 = 3
	tag1 uint64 = 4
	tag2 uint64 = 5
	tag3 uint64 = 6
	tag4 uint64 = 7
)

// TaggedSleepCmd carries an identifier so we can track which yield it came from
type TaggedSleepCmd struct {
	Tag      string
	Duration time.Duration
}

func (TaggedSleepCmd) CmdID() dispatcher.CommandID { return CmdTaggedSleep }

// PoolCompleteCmd marks completion
type PoolCompleteCmd struct{ Value any }

func (PoolCompleteCmd) CmdID() dispatcher.CommandID { return CmdPoolComplete }

// taggedSleepHandler completes after delay, returning the tag
type taggedSleepHandler struct{}

func (h *taggedSleepHandler) Handle(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(TaggedSleepCmd)
	go func() {
		select {
		case <-time.After(c.Duration):
			receiver.CompleteYield(tag, c.Tag, nil)
		case <-ctx.Done():
		}
	}()
	return nil
}

// poolCompleteHandler completes immediately (async to avoid race)
type poolCompleteHandler struct{}

func (h *poolCompleteHandler) Handle(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(PoolCompleteCmd)
	go receiver.CompleteYield(tag, c.Value, nil)
	return nil
}

// testDispatcher dispatches test commands
type testDispatcher struct {
	handlers map[dispatcher.CommandID]dispatcher.Handler
}

func newTestDispatcher() *testDispatcher {
	return &testDispatcher{
		handlers: map[dispatcher.CommandID]dispatcher.Handler{
			CmdTaggedSleep:  &taggedSleepHandler{},
			CmdPoolComplete: &poolCompleteHandler{},
		},
	}
}

func (d *testDispatcher) Dispatch(cmd dispatcher.Command) dispatcher.Handler {
	return d.handlers[cmd.CmdID()]
}

// poolTestContext creates context with frame for pool tests
func poolTestContext() context.Context {
	ctx, _ := ctxapi.AcquireFrameContext(context.Background())
	_ = runtime.SetFramePID(ctx, pid.PID{UniqID: "pool-test"})
	return ctx
}

// SequentialYieldPoolProcess simulates two coroutines yielding at different times.
type SequentialYieldPoolProcess struct {
	step          int
	results       []string
	mu            sync.Mutex
	totalExpected int
}

func (p *SequentialYieldPoolProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	p.totalExpected = 2
	return nil
}

func (p *SequentialYieldPoolProcess) Step(events []process.Event, out *process.StepOutput) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.step == 0 {
		p.step++
		out.Yield(TaggedSleepCmd{Tag: "A", Duration: 100 * time.Millisecond}, tagA)
		out.Yield(TaggedSleepCmd{Tag: "B", Duration: 10 * time.Millisecond}, tagB)
		out.Continue()
		return nil
	}

	for _, ev := range events {
		if ev.Type == process.EventYieldComplete && ev.Data != nil {
			tag := ev.Data.(string)
			p.results = append(p.results, tag)
		}
	}

	if len(p.results) >= p.totalExpected {
		out.Done(payload.New(p.results))
		return nil
	}

	out.Continue()
	return nil
}

func (p *SequentialYieldPoolProcess) Close()                    {}
func (p *SequentialYieldPoolProcess) Send(*relay.Package) error { return nil }

// TestPoolIndexCorrelationBreaks proves index-based correlation is broken in pool executor.
func TestPoolIndexCorrelationBreaks(t *testing.T) {
	d := newTestDispatcher()

	// Test with Static pool
	pool, err := NewStatic(func() (process.Process, error) {
		return &SequentialYieldPoolProcess{}, nil
	}, d, Config{Workers: 2})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	pool.Start()
	defer pool.Stop()

	result, err := pool.Call(poolTestContext(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}

	pl := result.Value

	results, ok := pl.Data().([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", pl.Data())
	}

	t.Logf("Result order: %v", results)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// CORRECT: results should be ["B", "A"] (completion order)
	// BUG: results are ["A", "B"] (index order)
	if results[0] == "A" && results[1] == "B" {
		t.Log("BUG CONFIRMED: Results are in index order, not completion order")
	}

	if results[0] != "B" || results[1] != "A" {
		t.Errorf("Expected [B, A] (completion order), got %v", results)
	}
}

// StaggeredYieldPoolProcess simulates yields across multiple steps
type StaggeredYieldPoolProcess struct {
	step        int
	results     map[string]bool
	resultOrder []string
	mu          sync.Mutex
}

func (p *StaggeredYieldPoolProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	p.results = make(map[string]bool)
	return nil
}

func (p *StaggeredYieldPoolProcess) Step(events []process.Event, out *process.StepOutput) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var gotB bool
	for _, ev := range events {
		if ev.Type == process.EventYieldComplete && ev.Data != nil {
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
		p.step++
		out.Yield(TaggedSleepCmd{Tag: "A", Duration: 200 * time.Millisecond}, tagA)
		out.Yield(TaggedSleepCmd{Tag: "B", Duration: 10 * time.Millisecond}, tagB)
		out.Continue()
		return nil

	case 1:
		if gotB {
			p.step++
			out.Yield(TaggedSleepCmd{Tag: "C", Duration: 50 * time.Millisecond}, tagC)
			out.Continue()
			return nil
		}
		out.Continue()
		return nil

	default:
		if len(p.results) >= 3 {
			out.Done(payload.New(p.resultOrder))
			return nil
		}
		out.Continue()
		return nil
	}
}

func (p *StaggeredYieldPoolProcess) Close()                    {}
func (p *StaggeredYieldPoolProcess) Send(*relay.Package) error { return nil }

// TestPoolStaggeredYields_IndexCollision tests yields spanning multiple steps.
// Scenario: Step 0 yields A (slow 200ms) and B (fast 10ms).
// B completes, Step 1 yields C (50ms) while A still pending.
// Expected order: [B, C, A] based on timing.
func TestPoolStaggeredYields_IndexCollision(t *testing.T) {
	d := newTestDispatcher()

	var proc *StaggeredYieldPoolProcess
	pool, err := NewStatic(func() (process.Process, error) {
		proc = &StaggeredYieldPoolProcess{}
		return proc, nil
	}, d, Config{Workers: 2})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	pool.Start()
	defer pool.Stop()

	result, err := pool.Call(poolTestContext(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}

	order, ok := result.Value.Data().([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", result.Value.Data())
	}

	t.Logf("Order: %v", order)

	// Expected: [B, C, A] based on timing
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

// TestPoolInlineIndexCorrelation tests the Inline pool type
func TestPoolInlineIndexCorrelation(t *testing.T) {
	d := newTestDispatcher()

	pool, err := NewInline(func() (process.Process, error) {
		return &SequentialYieldPoolProcess{}, nil
	}, d)
	if err != nil {
		t.Fatalf("NewInline: %v", err)
	}
	pool.Start()
	defer pool.Stop()

	result, err := pool.Call(poolTestContext(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}

	pl := result.Value

	results, ok := pl.Data().([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", pl.Data())
	}

	t.Logf("Inline pool result order: %v", results)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0] != "B" || results[1] != "A" {
		t.Errorf("Expected [B, A] (completion order), got %v", results)
	}
}

// TestPoolLazyIndexCorrelation tests the Lazy pool type
func TestPoolLazyIndexCorrelation(t *testing.T) {
	d := newTestDispatcher()

	pool, err := NewLazy(func() (process.Process, error) {
		return &SequentialYieldPoolProcess{}, nil
	}, d, LazyConfig{MaxWorkers: 2})
	if err != nil {
		t.Fatalf("NewLazy: %v", err)
	}
	pool.Start()
	defer pool.Stop()

	result, err := pool.Call(poolTestContext(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}

	pl := result.Value

	results, ok := pl.Data().([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", pl.Data())
	}

	t.Logf("Lazy pool result order: %v", results)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0] != "B" || results[1] != "A" {
		t.Errorf("Expected [B, A] (completion order), got %v", results)
	}
}

// ConcurrentYieldsProcess tests race conditions in multi-yield completion
type ConcurrentYieldsProcess struct {
	resultsReceived atomic.Int32
	expectedCount   int
}

func (p *ConcurrentYieldsProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	p.expectedCount = 4
	return nil
}

func (p *ConcurrentYieldsProcess) Step(events []process.Event, out *process.StepOutput) error {
	for _, ev := range events {
		if ev.Type == process.EventYieldComplete && ev.Data != nil {
			p.resultsReceived.Add(1)
		}
	}

	// First step: yield multiple commands that complete at same time
	if p.resultsReceived.Load() == 0 {
		// All complete at roughly same time - race condition test
		out.Yield(TaggedSleepCmd{Tag: "1", Duration: 5 * time.Millisecond}, tag1)
		out.Yield(TaggedSleepCmd{Tag: "2", Duration: 5 * time.Millisecond}, tag2)
		out.Yield(TaggedSleepCmd{Tag: "3", Duration: 5 * time.Millisecond}, tag3)
		out.Yield(TaggedSleepCmd{Tag: "4", Duration: 5 * time.Millisecond}, tag4)
		out.Continue()
		return nil
	}

	// Check if all received
	if int(p.resultsReceived.Load()) >= p.expectedCount {
		out.Done(payload.New(int(p.resultsReceived.Load())))
		return nil
	}

	out.Continue()
	return nil
}

func (p *ConcurrentYieldsProcess) Close()                    {}
func (p *ConcurrentYieldsProcess) Send(*relay.Package) error { return nil }

// TestPoolConcurrentYieldCompletion tests race conditions when multiple yields complete simultaneously
func TestPoolConcurrentYieldCompletion(t *testing.T) {
	d := newTestDispatcher()

	pool, err := NewStatic(func() (process.Process, error) {
		return &ConcurrentYieldsProcess{}, nil
	}, d, Config{Workers: 4})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	pool.Start()
	defer pool.Stop()

	// Run multiple times to catch race conditions
	for i := 0; i < 10; i++ {
		result, err := pool.Call(poolTestContext(), "test", nil)
		if err != nil {
			t.Fatalf("Call %d: %v", i, err)
		}
		if result.Error != nil {
			t.Fatalf("Call %d result error: %v", i, result.Error)
		}

		count, ok := result.Value.Data().(int)
		if !ok {
			t.Fatalf("Call %d: expected int, got %T", i, result.Value.Data())
		}

		if count != 4 {
			t.Errorf("Call %d: expected 4 results, got %d", i, count)
		}
	}
}

// TestPoolRaceConditionWithRaceDetector should be run with -race flag
// This test specifically tries to trigger the race condition in slot access
func TestPoolRaceConditionWithRaceDetector(t *testing.T) {
	d := newTestDispatcher()

	// Use multiple workers and concurrent calls to maximize race chance
	pool, err := NewStatic(func() (process.Process, error) {
		return &ConcurrentYieldsProcess{}, nil
	}, d, Config{Workers: 8})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	pool.Start()
	defer pool.Stop()

	var wg sync.WaitGroup
	const concurrent = 20

	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				result, err := pool.Call(poolTestContext(), "test", nil)
				if err != nil {
					t.Errorf("Worker %d Call %d: %v", id, j, err)
					return
				}
				if result.Error != nil {
					t.Errorf("Worker %d Call %d result error: %v", id, j, result.Error)
				}
			}
		}(i)
	}

	wg.Wait()
}
