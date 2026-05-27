// SPDX-License-Identifier: MPL-2.0

package time_test

import (
	"context"
	"sync"
	"testing"
	stdtime "time"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/lua/engine"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	"github.com/wippyai/runtime/system/clock"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
)

// testHostID is the relay host the actor scheduler registers under. The
// test PID's Host matches it so clock fire packages route back to the
// running process.
const testHostID = "time.test"

type testScheduler struct {
	*actor.Scheduler
	clock   *clock.Dispatcher
	node    *sysrelay.Node
	pending map[string]chan *runtime.Result
	mu      sync.Mutex
}

func (ts *testScheduler) Stop() {
	ts.Scheduler.Stop(context.Background())
	if ts.clock != nil {
		_ = ts.clock.Stop(context.Background())
	}
}

func (ts *testScheduler) OnStart(context.Context, pid.PID, process.Process) error { return nil }

func (ts *testScheduler) OnComplete(_ context.Context, p pid.PID, result *runtime.Result) {
	ts.mu.Lock()
	ch, ok := ts.pending[p.UniqID]
	if ok {
		delete(ts.pending, p.UniqID)
	}
	ts.mu.Unlock()
	if ok {
		ch <- result
	}
}

func (ts *testScheduler) Execute(ctx context.Context, p pid.PID, proc process.Process, method string, input payload.Payloads) (*runtime.Result, error) {
	resultCh := make(chan *runtime.Result, 1)

	ts.mu.Lock()
	ts.pending[p.UniqID] = resultCh
	ts.mu.Unlock()

	ctx = ts.withRelay(ctx)

	_, err := ts.Submit(ctx, p, proc, method, input)
	if err != nil {
		ts.mu.Lock()
		delete(ts.pending, p.UniqID)
		ts.mu.Unlock()
		return nil, err
	}

	select {
	case result := <-resultCh:
		return result, nil
	case <-ctx.Done():
		ts.mu.Lock()
		delete(ts.pending, p.UniqID)
		ts.mu.Unlock()
		return nil, ctx.Err()
	}
}

func newTestScheduler() *testScheduler {
	ts := &testScheduler{
		pending: make(map[string]chan *runtime.Result),
	}
	reg := scheduler.NewRegistry()
	clockSvc := clock.NewDispatcher()
	clockSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		reg.Register(id, h)
	})
	ts.clock = clockSvc
	opts := []actor.Option{
		actor.WithWorkers(4),
		actor.WithLifecycle(ts),
	}
	ts.Scheduler = actor.NewScheduler(reg, opts...)

	// Mirror production relay wiring: the clock dispatcher fires by
	// node.Send(pkg), routed by Target.Host to a registered host. The
	// actor scheduler is itself a relay.Receiver that delivers a package
	// into the target process's queue and wakes a worker to Step it, so it
	// plays the host role the function pool fills in production.
	ts.node = sysrelay.NewNode("time-test-node")
	if err := ts.node.RegisterHost(testHostID, ts.Scheduler); err != nil {
		panic(err)
	}
	return ts
}

// withRelay attaches the relay node so the clock dispatcher can schedule and
// fire timers. relay.WithNode needs an AppContext on ctx; the harness opens
// frame contexts off context.Background() (no AppContext), so seed one while
// preserving the caller's frame context (PID, frame values).
func (ts *testScheduler) withRelay(ctx context.Context) context.Context {
	if ctxapi.AppFromContext(ctx) == nil {
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
	}
	return relayapi.WithNode(ctx, ts.node)
}

func testPID() pid.PID {
	p := pid.PID{Host: testHostID, UniqID: "time-test"}
	return p.Precomputed()
}

// openTimeFrameCtx opens a frame context with the test PID set. time.after /
// time.timer / time.ticker read the frame PID to address clock fire packages
// back to the running process; without it the module raises immediately.
func openTimeFrameCtx(t *testing.T) (context.Context, ctxapi.FrameContext) {
	t.Helper()
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	if err := runtime.SetFramePID(ctx, testPID()); err != nil {
		t.Fatalf("set frame pid: %v", err)
	}
	return ctx, fc
}

// resultErr extracts the script error from a result for failure messages,
// distinguishing a nil result from a completed-with-error result.
func resultErr(r *runtime.Result) error {
	if r == nil {
		return context.DeadlineExceeded
	}
	return r.Error
}

func bindTimeModule(l *lua.LState) error {
	tbl, _ := timemod.Module.Build()
	l.SetGlobal(timemod.Module.Name, tbl)
	return nil
}

func newLuaProcessWithChannels(t *testing.T, script string) *engine.Process {
	t.Helper()
	proto, _ := lua.CompileString(script, "test.lua")
	proc, err := engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) error {
			engine.LoadModuleDef(l, engine.ChannelModule)
			return nil
		}),
		engine.WithModuleBinder(bindTimeModule),
	)
	if err != nil {
		t.Fatalf("NewProcess failed: %v", err)
	}
	return proc
}

// TestTickerBasic tests basic ticker functionality with channel API
func TestTickerBasic(t *testing.T) {
	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	script := `
		local results = {}
		local ticker = time.ticker(10 * time.MILLISECOND)

		-- Receive 3 ticks via channel
		for i = 1, 3 do
			local tick = ticker:response():receive()
			table.insert(results, "tick")
		end

		ticker:stop()
		return #results
	`

	ctx, _ := openTimeFrameCtx(t)
	proc := newLuaProcessWithChannels(t, script)

	start := stdtime.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := stdtime.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if result.Error != nil {
		t.Fatalf("script failed: %v", result.Error)
	}

	// 3 ticks at 10ms must take at least ~30ms; a vacuous run (no real
	// fires) would return in microseconds.
	if elapsed < 25*stdtime.Millisecond {
		t.Fatalf("received 3 ticks in %v: ticker is not firing", elapsed)
	}

	t.Logf("Ticker basic: received ticks in %v", elapsed)
}

// TestMultipleTickersStaggered tests staggered tickers
func TestMultipleTickersStaggered(t *testing.T) {
	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	script := `
		local order = {}
		local ticker1 = time.ticker(5 * time.MILLISECOND)
		local ticker2 = time.ticker(12 * time.MILLISECOND)
		local ticker3 = time.ticker(20 * time.MILLISECOND)
		local done = channel.new(3)

		-- Collect from each ticker in separate coroutines
		coroutine.spawn(function()
			for i = 1, 3 do
				ticker1:response():receive()
				table.insert(order, 1)
			end
			done:send(1)
		end)

		coroutine.spawn(function()
			for i = 1, 2 do
				ticker2:response():receive()
				table.insert(order, 2)
			end
			done:send(2)
		end)

		coroutine.spawn(function()
			ticker3:response():receive()
			table.insert(order, 3)
			done:send(3)
		end)

		-- Wait for all
		for i = 1, 3 do
			done:receive()
		end

		ticker1:stop()
		ticker2:stop()
		ticker3:stop()

		return #order
	`

	ctx, _ := openTimeFrameCtx(t)
	proc := newLuaProcessWithChannels(t, script)

	start := stdtime.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := stdtime.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if result.Error != nil {
		t.Fatalf("script failed: %v", result.Error)
	}

	// ticker1 fires 3x at 5ms, the loop only returns after every coroutine
	// drains its ticker; the slowest path (ticker1) needs ~15ms of real
	// fires.
	if elapsed < 12*stdtime.Millisecond {
		t.Fatalf("staggered tickers completed in %v: not firing", elapsed)
	}

	t.Logf("Multiple staggered tickers: completed in %v", elapsed)
}

// TestTickerStop tests ticker stop mid-stream
func TestTickerStop(t *testing.T) {
	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	script := `
		local ticker = time.ticker(5 * time.MILLISECOND)
		local count = 0

		-- Collect a few ticks
		for i = 1, 3 do
			ticker:response():receive()
			count = count + 1
		end

		-- Stop the ticker
		ticker:stop()

		return count
	`

	ctx, _ := openTimeFrameCtx(t)
	proc := newLuaProcessWithChannels(t, script)

	start := stdtime.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := stdtime.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if result.Error != nil {
		t.Fatalf("script failed: %v", result.Error)
	}

	// 3 ticks at 5ms require ~15ms of real fires before stop.
	if elapsed < 12*stdtime.Millisecond {
		t.Fatalf("3 ticks at 5ms completed in %v: not firing", elapsed)
	}

	t.Logf("Ticker stop: completed in %v", elapsed)
}

// TestTickerCleanupOnProcessExit tests that tickers are cleaned up when process exits
func TestTickerCleanupOnProcessExit(t *testing.T) {
	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	// Get initial ticker count
	initialCount := sched.clock.TickerCount()
	t.Logf("Initial ticker count: %d", initialCount)

	script := `
		-- Create tickers but DON'T stop them
		local ticker1 = time.ticker(100 * time.MILLISECOND)
		local ticker2 = time.ticker(200 * time.MILLISECOND)
		local ticker3 = time.ticker(300 * time.MILLISECOND)

		-- Just receive one tick to prove they work
		ticker1:response():receive()

		-- Exit WITHOUT calling stop() - cleanup should happen automatically
		return "done"
	`

	ctx, fc := openTimeFrameCtx(t)
	proc := newLuaProcessWithChannels(t, script)

	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if result.Error != nil {
		t.Fatalf("script failed: %v", result.Error)
	}

	// Release frame context - this triggers cleanup
	ctxapi.ReleaseFrameContext(fc)

	// Give a moment for cleanup
	stdtime.Sleep(10 * stdtime.Millisecond)

	// Check ticker count - should be back to initial
	finalCount := sched.clock.TickerCount()
	t.Logf("Final ticker count: %d", finalCount)

	if finalCount != initialCount {
		t.Errorf("Ticker leak: started with %d, ended with %d (expected %d)", initialCount, finalCount, initialCount)
	}
}

// TestTimerCleanupOnProcessExit tests that timers are cleaned up when process exits
func TestTimerCleanupOnProcessExit(t *testing.T) {
	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	// Get initial timer count
	initialCount := sched.clock.TimerCount()
	t.Logf("Initial timer count: %d", initialCount)

	script := `
		-- Create timers with long durations but DON'T wait for them
		local timer1 = time.timer(10 * time.SECOND)
		local timer2 = time.timer(20 * time.SECOND)
		local timer3 = time.timer(30 * time.SECOND)

		-- Exit immediately WITHOUT stopping timers - cleanup should happen
		return "done"
	`

	ctx, fc := openTimeFrameCtx(t)
	proc := newLuaProcessWithChannels(t, script)

	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if result.Error != nil {
		t.Fatalf("script failed: %v", result.Error)
	}

	// Release frame context - this triggers cleanup
	ctxapi.ReleaseFrameContext(fc)

	// Give a moment for cleanup
	stdtime.Sleep(10 * stdtime.Millisecond)

	// Check timer count - should be back to initial
	finalCount := sched.clock.TimerCount()
	t.Logf("Final timer count: %d", finalCount)

	if finalCount != initialCount {
		t.Errorf("Timer leak: started with %d, ended with %d (expected %d)", initialCount, finalCount, initialCount)
	}
}
