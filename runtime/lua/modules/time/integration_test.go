package time_test

import (
	"context"
	"sync"
	"testing"
	stdtime "time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/lua/engine"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	"github.com/wippyai/runtime/system/clock"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
	lua "github.com/yuin/gopher-lua"
)

type testScheduler struct {
	*actor.Scheduler
	clock   *clock.Dispatcher
	mu      sync.Mutex
	pending map[string]chan *runtime.Result
}

func (ts *testScheduler) Stop() {
	ts.Scheduler.Stop()
	if ts.clock != nil {
		_ = ts.clock.Stop(context.Background())
	}
}

func (ts *testScheduler) OnStart(context.Context, relay.PID, process.Process) {}

func (ts *testScheduler) OnComplete(_ context.Context, pid relay.PID, result *runtime.Result) {
	ts.mu.Lock()
	ch, ok := ts.pending[pid.UniqID]
	if ok {
		delete(ts.pending, pid.UniqID)
	}
	ts.mu.Unlock()
	if ok {
		ch <- result
	}
}

func (ts *testScheduler) Execute(ctx context.Context, pid relay.PID, p actor.Process, method string, input payload.Payloads) (*runtime.Result, error) {
	resultCh := make(chan *runtime.Result, 1)

	ts.mu.Lock()
	ts.pending[pid.UniqID] = resultCh
	ts.mu.Unlock()

	_, err := ts.Scheduler.Submit(ctx, pid, p, method, input)
	if err != nil {
		ts.mu.Lock()
		delete(ts.pending, pid.UniqID)
		ts.mu.Unlock()
		return nil, err
	}

	select {
	case result := <-resultCh:
		return result, nil
	case <-ctx.Done():
		ts.mu.Lock()
		delete(ts.pending, pid.UniqID)
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
	return ts
}

func testPID() relay.PID {
	return relay.PID{UniqID: "time-test"}
}

func bindTimeModule(l *lua.LState) {
	timemod.Module.Load(l)
}

func newLuaProcessWithChannels(script string) *engine.Process {
	proto, _ := lua.CompileString(script, "test.lua")
	return engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) { engine.ChannelModule.Load(l) }),
		engine.WithModuleBinder(bindTimeModule),
	)
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
			local tick = ticker:channel():receive()
			table.insert(results, "tick")
		end

		ticker:stop()
		return #results
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcessWithChannels(script)

	start := stdtime.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := stdtime.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	// Should take at least 30ms (3 * 10ms)
	if elapsed < 25*stdtime.Millisecond {
		t.Logf("Warning: elapsed time %v shorter than expected", elapsed)
	}

	t.Logf("Ticker basic: received ticks in %v", elapsed)
}

// TestTickerWithSelect tests ticker with channel.select (engine1 compatible API)
// TODO: Select with ticker channels requires architectural changes to support
// mixing dispatcher-based channels (TickerChannel) with in-memory channels (engine.Channel)
func TestTickerWithSelect(t *testing.T) {
	t.Skip("select with ticker channels not yet supported - requires dispatcher integration")
}

// TestOverlappingTickersSelect tests multiple tickers with select
// TODO: Select with ticker channels requires architectural changes
func TestOverlappingTickersSelect(t *testing.T) {
	t.Skip("select with ticker channels not yet supported - requires dispatcher integration")
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
				ticker1:channel():receive()
				table.insert(order, 1)
			end
			done:send(1)
		end)

		coroutine.spawn(function()
			for i = 1, 2 do
				ticker2:channel():receive()
				table.insert(order, 2)
			end
			done:send(2)
		end)

		coroutine.spawn(function()
			ticker3:channel():receive()
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

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcessWithChannels(script)

	start := stdtime.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := stdtime.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
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
			ticker:channel():receive()
			count = count + 1
		end

		-- Stop the ticker
		ticker:stop()

		return count
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcessWithChannels(script)

	start := stdtime.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := stdtime.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
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
		ticker1:channel():receive()

		-- Exit WITHOUT calling stop() - cleanup should happen automatically
		return "done"
	`

	ctx, fc := ctxapi.AcquireFrameContext(context.Background())
	proc := newLuaProcessWithChannels(script)

	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
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

	ctx, fc := ctxapi.AcquireFrameContext(context.Background())
	proc := newLuaProcessWithChannels(script)

	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
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
