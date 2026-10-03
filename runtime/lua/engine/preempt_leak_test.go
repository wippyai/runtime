// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	apiruntime "github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	scheduler "github.com/wippyai/runtime/system/scheduler/actor"
)

func heapInUse() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func settleGoroutines(want int) int {
	n := 0
	for i := 0; i < 200; i++ {
		runtime.GC()
		if n = runtime.NumGoroutine(); n <= want {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
	return n
}

func libraryCompiled(t *testing.T) *code.CompiledMain {
	t.Helper()
	baseID := registry.NewID("test", "base")
	midID := registry.NewID("test", "mid")
	mainID := registry.NewID("test", "main")
	return &code.CompiledMain{
		MainID: mainID,
		Imports: map[registry.ID][]code.Import{
			midID:  {{ID: baseID, Alias: "b"}},
			mainID: {{ID: midID, Alias: "m"}},
		},
		Dependencies: []code.CompiledProto{
			{Name: "base", Node: &code.Node{ID: baseID}, Proto: compileTestProto(t, `return { v = 1 }`)},
			{Name: "mid", Node: &code.Node{ID: midID}, Proto: compileTestProto(t, `return { v = b.v + 1 }`)},
		},
	}
}

// Repeated executions of one process neither add initializers, exported
// functions or registry entries nor grow the heap.
func TestPooledExecutionsDoNotGrowState(t *testing.T) {
	proc := newDependencyProcess(t, libraryCompiled(t), `return { main = function() return m.v end }`, 50)
	proc.EnablePreemption()

	run := func(n int) {
		for i := 0; i < n; i++ {
			if err := proc.Init(frameContext(), "main", nil); err != nil {
				t.Fatal(err)
			}
			var output process.StepOutput
			for j := 0; ; j++ {
				output.Reset()
				if err := proc.Step(nil, &output); err != nil {
					t.Fatal(err)
				}
				if output.Status() == process.StepDone {
					break
				}
				if j > 100 {
					t.Fatal("execution did not finish")
				}
			}
		}
	}

	run(50)
	list := initializersOf(proc.State())
	items, complete := len(list.items), len(list.onComplete)
	exported := len(proc.exported)
	top := proc.State().GetTop()
	heap0 := heapInUse()

	for batch := 0; batch < 4; batch++ {
		run(2000)
	}

	list = initializersOf(proc.State())
	if len(list.items) != items || len(list.onComplete) != complete {
		t.Fatalf("initializer list grew: items %d->%d, onComplete %d->%d", items, len(list.items), complete, len(list.onComplete))
	}
	if len(proc.exported) != exported {
		t.Fatalf("exported cache grew: %d->%d", exported, len(proc.exported))
	}
	if got := proc.State().GetTop(); got != top {
		t.Fatalf("main stack grew: %d->%d", top, got)
	}
	if len(proc.threads) != 0 {
		t.Fatalf("%d threads retained", len(proc.threads))
	}
	heap1 := heapInUse()
	if heap1 > heap0+heap0/2+1<<20 {
		t.Fatalf("heap grew from %d to %d over 8000 executions", heap0, heap1)
	}
}

type finalizerProbe struct{ collected *atomic.Int64 }

// Process.Close drops every reference this package adds, so a closed process
// keeps nothing of its Lua state alive and its fields are reset for reuse.
func TestCloseReleasesPreemptedProcessState(t *testing.T) {
	var collected atomic.Int64
	const total = 200
	baseline := runtime.NumGoroutine()

	for i := 0; i < total; i++ {
		probe := &finalizerProbe{collected: &collected}
		runtime.SetFinalizer(probe, func(p *finalizerProbe) { p.collected.Add(1) })
		binder := wrapBinder(func(l *lua.LState) {
			ud := l.NewUserData()
			ud.Value = probe
			l.SetGlobal("probe", ud)
		})
		entry := luaapi.ExecutionBudgets{TickBudget: 20, TickBudgetSet: true, MaxSteps: 1000, MaxStepsSet: true}
		proc := mustNewProcess(t,
			WithModuleBinder(binder),
			WithProcessExecutionBudgets(entry),
			WithScript(`
				local t = {}
				local co = coroutine.wrap(function() while true do t[#t+1] = probe coroutine.yield() end end)
				return { main = function() for i = 1, 1e9 do co() end end }
			`, "spin.lua"))
		proc.EnablePreemption()
		if err := proc.Init(frameContext(), "main", nil); err != nil {
			t.Fatal(err)
		}
		var output process.StepOutput
		for j := 0; j < 5; j++ {
			output.Reset()
			if err := proc.Step(nil, &output); err != nil {
				t.Fatal(err)
			}
			if output.Status() != process.StepPreempted {
				t.Fatalf("expected preemption, got %v", output.Status())
			}
		}
		proc.Close()

		if proc.preempted != nil || proc.exportFn != nil || proc.exported != nil || proc.steps != 0 ||
			proc.budgets != (luaapi.ExecutionBudgets{}) || proc.entryBudgets != (luaapi.ExecutionBudgets{}) || proc.preemptive {
			t.Fatalf("closed process retains state: %+v", proc)
		}
		v := reflect.ValueOf(proc).Elem()
		for _, name := range []string{"mainTask", "state", "factory", "upgradeRequest", "pendingOutdated", "result", "execErr"} {
			if f := v.FieldByName(name); f.IsValid() && !f.IsNil() {
				t.Fatalf("closed process retains %s", name)
			}
		}
	}

	deadline := time.Now().Add(10 * time.Second)
	for collected.Load() < total && time.Now().Before(deadline) {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
	if got := collected.Load(); got != total {
		t.Fatalf("%d of %d closed processes were collected", got, total)
	}
	if got := settleGoroutines(baseline); got > baseline {
		t.Fatalf("goroutines grew from %d to %d", baseline, got)
	}
}

// Closing a process with drained initializers, a preempted task and queued
// messages across many cycles keeps the heap flat.
func TestSpawnPreemptCloseCyclesKeepHeapFlat(t *testing.T) {
	compiled := libraryCompiled(t)
	cycle := func(n int) {
		for i := 0; i < n; i++ {
			binder := NewProcessFactory(nil).isolationBinder(compiled, newProcessConfig(), nil, nil, nil, nil)
			proc := mustNewProcess(t,
				WithModuleBinder(wrapBinder(func(l *lua.LState) { lua.OpenErrors(l) })),
				WithModuleBinder(binder),
				WithProcessExecutionBudgets(luaapi.ExecutionBudgets{TickBudget: 10, TickBudgetSet: true}),
				WithScript(fmt.Sprintf(`return { main = function() local n = %d while true do n = n + m.v end end }`, i), "m.lua"))
			proc.EnablePreemption()
			if err := proc.Init(frameContext(), "main", nil); err != nil {
				t.Fatal(err)
			}
			var output process.StepOutput
			for j := 0; j < 6; j++ {
				output.Reset()
				if err := proc.Step(nil, &output); err != nil {
					t.Fatal(err)
				}
			}
			proc.Close()
		}
	}
	cycle(200)
	heap0 := heapInUse()
	for batch := 0; batch < 4; batch++ {
		cycle(500)
	}
	heap1 := heapInUse()
	if heap1 > heap0+heap0/2+1<<20 {
		t.Fatalf("heap grew from %d to %d over 2000 cycles", heap0, heap1)
	}
}

type closeCounting struct {
	*Process
	closed *atomic.Int64
}

func (c *closeCounting) Close() {
	c.closed.Add(1)
	c.Process.Close()
}

func (c *closeCounting) Send(*relay.Package) error { return nil }

// Actors that finish, exceed max_steps or are terminated while preempting are
// all closed, unregistered and leave no goroutines behind.
func TestSchedulerLuaActorExitsReleaseEverything(t *testing.T) {
	baseline := runtime.NumGoroutine()
	var closed atomic.Int64
	executor := newBenchExecutorWithOptions(newMockRegistry(), scheduler.WithWorkers(4))
	executor.Start()

	finish := compileTestProto(t, `local s = 0 for i = 1, 3000 do s = s + i end return s`)
	spin := compileTestProto(t, `while true do end`)
	budgets := luaapi.ExecutionBudgets{TickBudget: 50, TickBudgetSet: true}

	const total = 300
	var wg sync.WaitGroup
	results := make([]error, total)
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			proto := finish
			if i%3 != 0 {
				proto = spin
			}
			proc := mustNewProcess(t, WithProto(proto), WithProcessExecutionBudgets(budgets))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			frame, fc := ctxapi.OpenFrameContext(ctx)
			switch i % 3 {
			case 1:
				_ = fc.Set(apiruntime.FrameLifecycleOptionsKey, attrs.Attributes(attrs.Bag{"max_steps": int64(10)}))
			case 2:
				time.AfterFunc(20*time.Millisecond, cancel)
			}
			result, err := executor.Execute(frame, newTestPID(fmt.Sprintf("a%d", i)), &closeCounting{Process: proc, closed: &closed}, "", nil)
			if err != nil {
				results[i] = err
				return
			}
			results[i] = result.Error
		}(i)
	}
	wg.Wait()

	for i, err := range results {
		switch i % 3 {
		case 0:
			if err != nil {
				t.Fatalf("actor %d: %v", i, err)
			}
		case 1:
			if !errors.Is(err, process.ErrStepLimitExceeded) {
				t.Fatalf("actor %d: expected step limit, got %v", i, err)
			}
		case 2:
			if err == nil {
				t.Fatalf("actor %d: expected termination", i)
			}
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for closed.Load() < total && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := closed.Load(); got != total {
		t.Fatalf("closed %d of %d actors", got, total)
	}
	executor.Stop()
	if got := settleGoroutines(baseline); got > baseline {
		t.Fatalf("goroutines grew from %d to %d", baseline, got)
	}
}
