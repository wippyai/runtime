// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	scheduler "github.com/wippyai/runtime/system/scheduler/actor"
)

var _ process.Preemptible = (*Process)(nil)

// stepPreempted runs proc until it completes and returns its result value and
// the number of steps that reported StepPreempted.
func stepPreempted(t *testing.T, proc *Process, maxSteps int) (lua.LValue, int) {
	t.Helper()
	var output process.StepOutput
	preempts := 0
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		switch output.Status() {
		case process.StepPreempted:
			preempts++
		case process.StepDone:
			if output.Result() == nil {
				t.Fatal("expected a result")
			}
			v, _ := output.Result().Data().(lua.LValue)
			return v, preempts
		case process.StepContinue:
		default:
			t.Fatalf("step %d: unexpected status %v", i, output.Status())
		}
	}
	t.Fatalf("not done after %d steps (%d preemptions)", maxSteps, preempts)
	return nil, 0
}

// initPreemptProcess starts script with the given entry tick_budget under a
// scheduler that enables preemption; budget 0 leaves tick_budget unset.
func initPreemptProcess(t *testing.T, script string, budget int64) *Process {
	t.Helper()
	var budgets luaapi.ExecutionBudgets
	if budget != 0 {
		budgets = luaapi.ExecutionBudgets{TickBudget: budget, TickBudgetSet: true}
	}
	proc := mustNewProcess(t, WithScript(script, "preempt.lua"), WithProcessExecutionBudgets(budgets))
	proc.EnablePreemption()
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(proc.Close)
	return proc
}

func TestProcessPreemptsLongRunningStep(t *testing.T) {
	proc := initPreemptProcess(t, `
		local s = 0
		for i = 1, 100000 do s = s + i end
		return s
	`, 1000)

	v, preempts := stepPreempted(t, proc, 10000)
	if lua.LVAsNumber(v) != 5000050000 {
		t.Fatalf("expected 5000050000, got %v", v)
	}
	if preempts < 50 {
		t.Fatalf("expected the loop to be split into slices, got %d preemptions", preempts)
	}
}

func TestProcessNegativeTickBudgetRunsStepToCompletion(t *testing.T) {
	proc := initPreemptProcess(t, `
		local s = 0
		for i = 1, 100000 do s = s + i end
		return s
	`, -1)

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}
	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone in one step, got %v", output.Status())
	}
}

// The budget covers the whole step: coroutines that did not get to run
// before it ran out are deferred to the next step rather than each getting a
// fresh budget.
func TestProcessPreemptionBudgetIsPerStep(t *testing.T) {
	proc := initPreemptProcess(t, `
		local done = 0
		local function work()
			local s = 0
			for i = 1, 20000 do s = s + i end
			done = done + 1
		end
		for _ = 1, 8 do coroutine.spawn(work) end
		while done < 8 do coroutine.yield() end
		return done
	`, 500)

	var output process.StepOutput
	steps := 0
	for {
		steps++
		if steps > 100000 {
			t.Fatal("process did not complete")
		}
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatal(err)
		}
		if output.Status() == process.StepDone {
			break
		}
	}
	v, _ := output.Result().Data().(lua.LValue)
	if lua.LVAsNumber(v) != 8 {
		t.Fatalf("expected 8 finished coroutines, got %v", v)
	}
	// 8 coroutines * 20000 iterations need at least 320 slices of 500 ticks.
	if steps < 320 {
		t.Fatalf("expected per-step slicing across coroutines, completed in %d steps", steps)
	}
}

func TestProcessPreemptionKeepsSpawnedCoroutinesConsistent(t *testing.T) {
	proc := initPreemptProcess(t, `
		local total = 0
		local function add(n)
			local s = 0
			for i = 1, n do s = s + i end
			total = total + s
		end
		coroutine.spawn(function() add(5000) end)
		coroutine.spawn(function() add(3000) end)
		add(1000)
		while total ~= 12502500 + 4501500 + 500500 do coroutine.yield() end
		return total
	`, 300)

	v, preempts := stepPreempted(t, proc, 100000)
	if lua.LVAsNumber(v) != 17504500 {
		t.Fatalf("expected 17504500, got %v", v)
	}
	if preempts == 0 {
		t.Fatal("expected preemption")
	}
}

// On a single scheduler worker, a Lua actor stuck in an endless loop is
// preempted so another actor still runs to completion.
func TestSpinningActorDoesNotStarveOtherActors(t *testing.T) {
	executor := newBenchExecutorWithOptions(newMockRegistry(),
		scheduler.WithWorkers(1))
	executor.Start()
	defer executor.Stop()

	spinProto, err := lua.CompileString(`while true do end`, "spin.lua")
	if err != nil {
		t.Fatal(err)
	}
	quickProto, err := lua.CompileString(`local s = 0 for i = 1, 1000 do s = s + i end return s`, "quick.lua")
	if err != nil {
		t.Fatal(err)
	}

	spinCtx, stopSpin := context.WithCancel(context.Background())
	defer stopSpin()
	spinFrame, _ := ctxapi.OpenFrameContext(spinCtx)
	spin := &enteredProcess{Process: mustNewProcess(t, WithProto(spinProto)), entered: make(chan struct{})}
	spinDone := make(chan struct{})
	go func() {
		defer close(spinDone)
		_, _ = executor.Execute(spinFrame, newTestPID("spin"), spin, "", nil)
	}()
	select {
	case <-spin.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("spinning actor did not start")
	}

	quickCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	quickFrame, _ := ctxapi.OpenFrameContext(quickCtx)
	result, err := executor.Execute(quickFrame, newTestPID("quick"), &mockProcess{Process: mustNewProcess(t, WithProto(quickProto))}, "", nil)
	if err != nil {
		t.Fatalf("quick actor did not complete while another actor spins: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("quick actor failed: %v", result.Error)
	}
	if v, _ := result.Value.Data().(lua.LValue); lua.LVAsNumber(v) != 500500 {
		t.Fatalf("expected 500500, got %v", result.Value.Data())
	}

	select {
	case <-spinDone:
		t.Fatal("spinning actor finished unexpectedly")
	default:
	}
	stopSpin()
	select {
	case <-spinDone:
	case <-time.After(5 * time.Second):
		t.Fatal("spinning actor did not stop after cancellation")
	}
}

// enteredProcess signals when its first step begins.
type enteredProcess struct {
	*Process
	entered chan struct{}
	started bool
}

func (p *enteredProcess) Step(events []process.Event, out *process.StepOutput) error {
	if !p.started {
		p.started = true
		close(p.entered)
	}
	return p.Process.Step(events, out)
}

// Without a scheduler that enables preemption a step runs to completion
// whatever the tick budget.
func TestProcessNotEnabledForPreemptionRunsStepToCompletion(t *testing.T) {
	proc := mustNewProcess(t, WithScript(`
		local s = 0
		for i = 1, 100000 do s = s + i end
		return s
	`, "preempt.lua"), WithProcessExecutionBudgets(luaapi.ExecutionBudgets{TickBudget: 10, TickBudgetSet: true}))
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}
	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone in one step, got %v", output.Status())
	}
}

func TestProcessDefaultTickBudget(t *testing.T) {
	proc := initPreemptProcess(t, `
		local s = 0
		for i = 1, 100000 do s = s + i end
		return s
	`, 0)
	_, preempts := stepPreempted(t, proc, 100000)
	// 100000 iterations at the default budget of 512 ticks.
	if want := 100000 / int(DefaultActorTickBudget); preempts < want {
		t.Fatalf("expected at least %d preemptions at the default budget, got %d", want, preempts)
	}
}

// initWithSpawnOptions starts script with spawn options in the frame context,
// as the host does for process.with_options(...) spawns.
func initWithSpawnOptions(t *testing.T, script string, entry luaapi.ExecutionBudgets, spawn attrs.Bag) (*Process, error) {
	t.Helper()
	proc := mustNewProcess(t, WithScript(script, "preempt.lua"), WithProcessExecutionBudgets(entry))
	proc.EnablePreemption()
	t.Cleanup(proc.Close)
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	if err := fc.Set(runtime.FrameLifecycleOptionsKey, attrs.Attributes(spawn)); err != nil {
		t.Fatal(err)
	}
	return proc, proc.Init(ctx, "", nil)
}

func TestProcessSpawnOptionsOverrideEntryTickBudget(t *testing.T) {
	script := `
		local s = 0
		for i = 1, 100000 do s = s + i end
		return s
	`
	entry := luaapi.ExecutionBudgets{TickBudget: 100, TickBudgetSet: true}
	proc, err := initWithSpawnOptions(t, script, entry, attrs.Bag{"tick_budget": int64(-1)})
	if err != nil {
		t.Fatal(err)
	}
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}
	if output.Status() != process.StepDone {
		t.Fatalf("spawn tick_budget=-1 must disable preemption, got %v", output.Status())
	}
}

func TestProcessRejectsInvalidSpawnOptions(t *testing.T) {
	cases := []attrs.Bag{
		{"tick_budget": "fast"},
		{"tick_budget": 1.5},
		{"max_steps": int64(-1)},
	}
	for _, spawn := range cases {
		if _, err := initWithSpawnOptions(t, `return 1`, luaapi.ExecutionBudgets{}, spawn); err == nil {
			t.Fatalf("expected %v to be rejected", spawn)
		}
	}
}

func TestProcessMaxStepsFailsProcess(t *testing.T) {
	script := `
		local s = 0
		for i = 1, 100000 do s = s + i end
		return s
	`
	entry := luaapi.ExecutionBudgets{TickBudget: 100, TickBudgetSet: true, MaxSteps: 5, MaxStepsSet: true}
	proc, err := initWithSpawnOptions(t, script, entry, nil)
	if err != nil {
		t.Fatal(err)
	}
	var output process.StepOutput
	for i := 0; i < 5; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if output.Status() != process.StepPreempted {
			t.Fatalf("step %d: expected StepPreempted, got %v", i, output.Status())
		}
	}
	output.Reset()
	if err := proc.Step(nil, &output); !errors.Is(err, process.ErrStepLimitExceeded) {
		t.Fatalf("expected ErrStepLimitExceeded, got %v", err)
	}
}
