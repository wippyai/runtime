// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/scheduler"
)

// Command IDs for correlation tests
const (
	CmdDelayedComplete dispatcher.CommandID = 100
	CmdTaggedSleep     dispatcher.CommandID = 200
)

// Yield correlation tags for tests
const (
	tagA  uint64 = 1
	tagB  uint64 = 2
	tagC  uint64 = 3
	tagY1 uint64 = 4
	tagX1 uint64 = 5
	tagX2 uint64 = 6
	tagX3 uint64 = 7
)

// DelayedCompleteCmd completes after a specified delay.
type DelayedCompleteCmd struct {
	Value any
	ID    int
	Delay time.Duration
}

func (DelayedCompleteCmd) CmdID() dispatcher.CommandID { return CmdDelayedComplete }

// DelayedHandler completes after delay, tracking completion order.
type DelayedHandler struct {
	completionOrder *[]int
	mu              *sync.Mutex
}

func (h *DelayedHandler) Handle(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
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

// TaggedSleepCmd carries an identifier for tracking which yield it came from.
type TaggedSleepCmd struct {
	Tag      string
	Duration time.Duration
}

func (TaggedSleepCmd) CmdID() dispatcher.CommandID { return CmdTaggedSleep }

// TaggedSleepHandler completes after delay, returning the tag.
type TaggedSleepHandler struct{}

func (h *TaggedSleepHandler) Handle(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
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

// TagTrackingCmd is a command that tracks Tag propagation.
type TagTrackingCmd struct {
	Value string
	ID    int
}

func (TagTrackingCmd) CmdID() dispatcher.CommandID { return CmdTaggedSleep }

// TagTrackingHandler completes with the original command data.
type TagTrackingHandler struct{}

func (h *TagTrackingHandler) Handle(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(TagTrackingCmd)
	go receiver.CompleteYield(tag, c.Value, nil)
	return nil
}

// AsyncTagTrackingHandler completes asynchronously after a delay.
type AsyncTagTrackingHandler struct {
	delay time.Duration
}

func (h *AsyncTagTrackingHandler) Handle(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
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

// MultiYieldProcess yields multiple commands in one Step, tracks resume order.
type MultiYieldProcess struct {
	ctx         context.Context
	yields      []DelayedCompleteCmd
	resumeOrder []int
	step        int
	mu          sync.Mutex
}

func (p *MultiYieldProcess) Init(ctx context.Context, _ string, input payload.Payloads) error {
	p.ctx = ctx
	if len(input) > 0 {
		p.yields = input[0].Data().([]DelayedCompleteCmd)
	}
	return nil
}

func (p *MultiYieldProcess) Step(events []process.Event, out *process.StepOutput) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.step == 0 {
		p.step++
		for _, cmd := range p.yields {
			out.Yield(cmd, uint64(cmd.ID))
		}
		out.Continue()
		return nil
	}

	for _, ev := range events {
		if ev.Type == process.EventYieldComplete && ev.Tag != 0 {
			p.resumeOrder = append(p.resumeOrder, int(ev.Tag))
		}
	}

	if len(p.resumeOrder) >= len(p.yields) {
		out.Yield(CompleteCmd{Value: p.resumeOrder}, 0)
		out.Done(nil)
		return nil
	}

	out.Continue()
	return nil
}

func (p *MultiYieldProcess) Send(*relay.Package) error { return nil }
func (p *MultiYieldProcess) Close()                    {}

// TestMultiYieldIndependentCompletion verifies that yields complete independently.
func TestMultiYieldIndependentCompletion(t *testing.T) {
	var completionOrder []int
	var mu sync.Mutex

	handler := &DelayedHandler{
		completionOrder: &completionOrder,
		mu:              &mu,
	}

	registry := scheduler.NewRegistry()
	registry.Register(CmdDelayedComplete, handler)
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pid.PID, _ *runtime.Result) {
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(2), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(context.Background())

	yields := []DelayedCompleteCmd{
		{ID: 1, Delay: 100 * time.Millisecond, Value: "slow"},
		{ID: 2, Delay: 10 * time.Millisecond, Value: "fast"},
	}

	proc := &MultiYieldProcess{}
	_, err := sched.Submit(context.Background(), testPID(), proc, "", testInput(yields))
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("process did not complete")
	}

	mu.Lock()
	handlerOrder := append([]int{}, completionOrder...)
	mu.Unlock()

	t.Logf("Handler completion order: %v", handlerOrder)
	if len(handlerOrder) != 2 || handlerOrder[0] != 2 || handlerOrder[1] != 1 {
		t.Errorf("Expected handler completion order [2, 1], got %v", handlerOrder)
	}

	proc.mu.Lock()
	resumeOrder := append([]int{}, proc.resumeOrder...)
	proc.mu.Unlock()

	t.Logf("Process resume order: %v", resumeOrder)

	if len(resumeOrder) != 2 {
		t.Errorf("Expected 2 resumes, got %d", len(resumeOrder))
	}

	if resumeOrder[0] != 2 || resumeOrder[1] != 1 {
		t.Errorf("Expected resume order [2, 1] (completion order), got %v (batched order)", resumeOrder)
	}
}

// SequentialYieldProcess simulates two coroutines yielding at different times.
type SequentialYieldProcess struct {
	ctx           context.Context
	results       []string
	step          int
	totalExpected int
	mu            sync.Mutex
}

func (p *SequentialYieldProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.ctx = ctx
	p.totalExpected = 2
	return nil
}

func (p *SequentialYieldProcess) Step(events []process.Event, out *process.StepOutput) error {
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

func (p *SequentialYieldProcess) Send(_ *relay.Package) error { return nil }
func (p *SequentialYieldProcess) Close()                      {}

// TestIndexCorrelationBreaks proves that index-based correlation delivers results
// in the wrong order when yields complete out of order.
func TestIndexCorrelationBreaks_ActorScheduler(t *testing.T) {
	registry := scheduler.NewRegistry()
	registry.Register(CmdTaggedSleep, &TaggedSleepHandler{})
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pid.PID, _ *runtime.Result) {
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(4), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(context.Background())

	proc := &SequentialYieldProcess{}
	_, err := sched.Submit(context.Background(), testPID(), proc, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

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

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0] == "A" && results[1] == "B" {
		t.Log("BUG CONFIRMED: Results are in index order, not completion order")
		t.Log("Expected: [B, A] (completion order)")
		t.Log("Got: [A, B] (index order - WRONG)")
	}

	if results[0] != "B" || results[1] != "A" {
		t.Errorf("Expected result order [B, A] (completion order), got %v", results)
	}
}

// StaggeredYieldProcess simulates a more complex scenario with staggered yields.
type StaggeredYieldProcess struct {
	ctx         context.Context
	results     map[string]bool
	resultOrder []string
	step        int
	mu          sync.Mutex
}

func (p *StaggeredYieldProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.ctx = ctx
	p.results = make(map[string]bool)
	return nil
}

func (p *StaggeredYieldProcess) Step(events []process.Event, out *process.StepOutput) error {
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

func (p *StaggeredYieldProcess) Send(_ *relay.Package) error { return nil }
func (p *StaggeredYieldProcess) Close()                      {}

// TestStaggeredYields_IndexCollision proves that index correlation breaks across steps.
func TestStaggeredYields_IndexCollision(t *testing.T) {
	registry := scheduler.NewRegistry()
	registry.Register(CmdTaggedSleep, &TaggedSleepHandler{})
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pid.PID, _ *runtime.Result) {
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(4), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(context.Background())

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

	if !results["A"] || !results["B"] || !results["C"] {
		t.Errorf("Missing results: A=%v, B=%v, C=%v", results["A"], results["B"], results["C"])
	}

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

// ParallelCoroutinesProcess simulates multiple coroutines running concurrently.
type ParallelCoroutinesProcess struct {
	ctx             context.Context
	xResults        []string
	yResults        []string
	yieldedXCount   int
	xYieldsExpected int
	mu              sync.Mutex
}

func (p *ParallelCoroutinesProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.ctx = ctx
	p.xYieldsExpected = 3
	return nil
}

func (p *ParallelCoroutinesProcess) Step(events []process.Event, out *process.StepOutput) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, ev := range events {
		if ev.Type == process.EventYieldComplete && ev.Data != nil {
			tag := ev.Data.(string)
			if len(tag) > 0 && tag[0] == 'X' {
				p.xResults = append(p.xResults, tag)
			} else if len(tag) > 0 && tag[0] == 'Y' {
				p.yResults = append(p.yResults, tag)
			}
		}
	}

	if len(p.xResults) >= p.xYieldsExpected && len(p.yResults) >= 1 {
		out.Done(payload.New(map[string][]string{
			"X": p.xResults,
			"Y": p.yResults,
		}))
		return nil
	}

	if p.yieldedXCount < p.xYieldsExpected {
		p.yieldedXCount++
		var yieldTag uint64
		var cmdTag string
		switch p.yieldedXCount {
		case 1:
			yieldTag, cmdTag = tagX1, "X1"
		case 2:
			yieldTag, cmdTag = tagX2, "X2"
		case 3:
			yieldTag, cmdTag = tagX3, "X3"
		}
		out.Yield(TaggedSleepCmd{Tag: cmdTag, Duration: 5 * time.Millisecond}, yieldTag)
		if p.yieldedXCount == 1 {
			out.Yield(TaggedSleepCmd{Tag: "Y1", Duration: 100 * time.Millisecond}, tagY1)
		}
		out.Continue()
		return nil
	}

	return nil
}

func (p *ParallelCoroutinesProcess) Send(_ *relay.Package) error { return nil }
func (p *ParallelCoroutinesProcess) Close()                      {}

// TestParallelCoroutines_IndexCorrelation proves that concurrent coroutine patterns
// break with index correlation.
func TestParallelCoroutines_IndexCorrelation(t *testing.T) {
	registry := scheduler.NewRegistry()
	registry.Register(CmdTaggedSleep, &TaggedSleepHandler{})
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pid.PID, _ *runtime.Result) {
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(4), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(context.Background())

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

	if len(xResults) != 3 {
		t.Errorf("Expected 3 X results, got %d: %v", len(xResults), xResults)
	}

	if len(yResults) != 1 || (len(yResults) > 0 && yResults[0] != "Y1") {
		t.Errorf("Expected Y results [Y1], got %v", yResults)
	}

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

// SingleYieldTagProcess yields one command at a time and verifies Tag is returned.
type SingleYieldTagProcess struct {
	ctx         context.Context
	step        int
	receivedTag uint64
	mu          sync.Mutex
}

func (p *SingleYieldTagProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.ctx = ctx
	return nil
}

func (p *SingleYieldTagProcess) Step(events []process.Event, out *process.StepOutput) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.step == 0 {
		p.step++
		out.Yield(TagTrackingCmd{ID: 1, Value: "test-value"}, 100)
		out.Continue()
		return nil
	}

	var data any
	for _, ev := range events {
		if ev.Type == process.EventYieldComplete {
			p.receivedTag = ev.Tag
			data = ev.Data
		}
	}

	out.Yield(CompleteCmd{Value: map[string]any{
		"receivedTag": p.receivedTag,
		"data":        data,
	}}, 0)
	out.Done(nil)
	return nil
}

func (p *SingleYieldTagProcess) Send(_ *relay.Package) error { return nil }
func (p *SingleYieldTagProcess) Close()                      {}

// TestSingleYieldTagPropagation verifies that the single-yield path
// correctly propagates the Tag back to the process.
func TestSingleYieldTagPropagation(t *testing.T) {
	registry := scheduler.NewRegistry()
	registry.Register(CmdTaggedSleep, &TagTrackingHandler{})
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool
	var result *runtime.Result

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pid.PID, res *runtime.Result) {
			result = res
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(context.Background())

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

	proc.mu.Lock()
	receivedTag := proc.receivedTag
	proc.mu.Unlock()

	if receivedTag != 100 {
		t.Errorf("expected Tag 100, got %v", receivedTag)
	}
}

// SequentialTagYieldProcess yields one command per Step across multiple Steps.
type SequentialTagYieldProcess struct {
	ctx          context.Context
	pendingTasks map[uint64]int
	results      []int
	step         int
	mu           sync.Mutex
}

func (p *SequentialTagYieldProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.ctx = ctx
	p.pendingTasks = make(map[uint64]int)
	return nil
}

func (p *SequentialTagYieldProcess) Step(events []process.Event, out *process.StepOutput) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, ev := range events {
		if ev.Type == process.EventYieldComplete && ev.Tag != 0 {
			if taskID, ok := p.pendingTasks[ev.Tag]; ok {
				p.results = append(p.results, taskID)
				delete(p.pendingTasks, ev.Tag)
			}
		}
	}

	if p.step < 3 {
		p.step++
		tag := uint64(p.step)
		p.pendingTasks[tag] = p.step

		out.Yield(TagTrackingCmd{ID: p.step, Value: "value"}, tag)
		out.Continue()
		return nil
	}

	if len(p.pendingTasks) > 0 {
		out.Continue()
		return nil
	}

	out.Yield(CompleteCmd{Value: p.results}, 0)
	out.Done(nil)
	return nil
}

func (p *SequentialTagYieldProcess) Send(_ *relay.Package) error { return nil }
func (p *SequentialTagYieldProcess) Close()                      {}

// TestSequentialYieldTagCorrelation verifies that when a process yields
// one command per Step across multiple Steps, each result is correctly
// correlated via Tag.
func TestSequentialYieldTagCorrelation(t *testing.T) {
	registry := scheduler.NewRegistry()
	registry.Register(CmdTaggedSleep, &TagTrackingHandler{})
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool
	var result *runtime.Result

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pid.PID, res *runtime.Result) {
			result = res
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(context.Background())

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

	proc.mu.Lock()
	results := append([]int{}, proc.results...)
	proc.mu.Unlock()

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d: %v", len(results), results)
	}
}

// StaggeredMultiYieldProcess simulates the Lua distributed_work scenario.
type StaggeredMultiYieldProcess struct {
	ctx           context.Context
	pendingYields map[uint64]bool
	completedTags []uint64
	yieldCount    int
	maxYields     int
	workers       int
	mu            sync.Mutex
}

func (p *StaggeredMultiYieldProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.ctx = ctx
	p.pendingYields = make(map[uint64]bool)
	p.workers = 3
	p.maxYields = 2
	return nil
}

func (p *StaggeredMultiYieldProcess) Step(events []process.Event, out *process.StepOutput) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var completedThisStep []uint64
	for _, ev := range events {
		if ev.Type == process.EventYieldComplete && ev.Tag != 0 {
			delete(p.pendingYields, ev.Tag)
			p.completedTags = append(p.completedTags, ev.Tag)
			completedThisStep = append(completedThisStep, ev.Tag)
		}
	}

	if p.yieldCount == 0 {
		for i := 0; i < p.workers; i++ {
			tag := uint64(i + 1)
			p.pendingYields[tag] = true
			p.yieldCount++
			out.Yield(TagTrackingCmd{ID: int(tag), Value: "initial"}, tag)
		}
		out.Continue()
		return nil
	}

	workerYields := make(map[int]int)
	for _, tag := range p.completedTags {
		workerID := ((int(tag) - 1) % p.workers) + 1
		workerYields[workerID]++
	}

	for _, tag := range completedThisStep {
		workerID := ((int(tag) - 1) % p.workers) + 1
		if workerYields[workerID] < p.maxYields {
			p.yieldCount++
			newTag := uint64(p.yieldCount)
			p.pendingYields[newTag] = true
			workerYields[workerID]++
			out.Yield(TagTrackingCmd{ID: int(newTag), Value: "subsequent"}, newTag)
		}
	}

	if len(p.pendingYields) == 0 && len(p.completedTags) >= p.workers*p.maxYields {
		out.Yield(CompleteCmd{Value: p.completedTags}, 0)
		out.Done(nil)
		return nil
	}

	out.Continue()
	return nil
}

func (p *StaggeredMultiYieldProcess) Send(_ *relay.Package) error { return nil }
func (p *StaggeredMultiYieldProcess) Close()                      {}

// TestStaggeredMultiYield tests the scenario where multiple concurrent yields
// are active, and as each completes, the process yields again.
func TestStaggeredMultiYield(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	registry := scheduler.NewRegistry()
	registry.Register(CmdTaggedSleep, &AsyncTagTrackingHandler{delay: 1 * time.Millisecond})
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool
	var result *runtime.Result

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pid.PID, res *runtime.Result) {
			result = res
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(context.Background())

	proc := &StaggeredMultiYieldProcess{}
	_, err := sched.Submit(context.Background(), testPID(), proc, "", nil)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	if !completed.Load() {
		proc.mu.Lock()
		pending := len(proc.pendingYields)
		completedCount := len(proc.completedTags)
		yields := proc.yieldCount
		proc.mu.Unlock()
		t.Fatalf("process did not complete - pending=%d, completed=%d, totalYields=%d (deadlock in staggered multi-yield)",
			pending, completedCount, yields)
	}

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	proc.mu.Lock()
	completedCount := len(proc.completedTags)
	proc.mu.Unlock()

	expected := 3 * 2
	if completedCount != expected {
		t.Errorf("expected %d completed tags, got %d", expected, completedCount)
	}
}

// TestAsyncSingleYieldTagPropagation verifies Tag propagation with async handlers.
func TestAsyncSingleYieldTagPropagation(t *testing.T) {
	registry := scheduler.NewRegistry()
	registry.Register(CmdTaggedSleep, &AsyncTagTrackingHandler{delay: 10 * time.Millisecond})
	registry.Register(CmdComplete, CompleteHandler())

	var completed atomic.Bool
	var result *runtime.Result

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pid.PID, res *runtime.Result) {
			result = res
			completed.Store(true)
		},
	}

	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	sched.Start()
	defer sched.Stop(context.Background())

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

	if receivedTag != 100 {
		t.Errorf("expected Tag 100, got %v", receivedTag)
	}
}
