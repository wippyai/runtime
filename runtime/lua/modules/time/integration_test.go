package time_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	stdtime "time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/lua/engine"
	timeyields "github.com/wippyai/runtime/runtime/lua/modules/time"
	"github.com/wippyai/runtime/system/clock"
	scheduler "github.com/wippyai/runtime/system/scheduler/actor"
	lua "github.com/yuin/gopher-lua"
)

type testScheduler struct {
	*scheduler.Scheduler
	clock   *clock.Dispatcher
	mu      sync.Mutex
	pending map[string]chan *runtime.Result
}

func (ts *testScheduler) Stop() {
	ts.Scheduler.Stop()
	if ts.clock != nil {
		ts.clock.Stop(context.Background())
	}
}

func (ts *testScheduler) OnStart(ctx context.Context, pid relay.PID, p process.Process) {}

func (ts *testScheduler) OnComplete(ctx context.Context, pid relay.PID, result *runtime.Result) {
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

func (ts *testScheduler) Execute(ctx context.Context, pid relay.PID, p scheduler.Process, method string, input payload.Payloads) (*runtime.Result, error) {
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

var testPIDCounter atomic.Int64

func uniqueTestPID() relay.PID {
	return relay.PID{UniqID: stdtime.Now().Format("20060102150405.000000000") + "-" + string(rune(testPIDCounter.Add(1)))}
}

func newTestScheduler(numWorkers int, opts ...scheduler.Option) *testScheduler {
	ts := &testScheduler{
		pending: make(map[string]chan *runtime.Result),
	}
	registry := scheduler.NewRegistry()
	clockSvc := clock.NewDispatcher()
	clockSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		registry.Register(id, h)
	})
	ts.clock = clockSvc
	opts = append([]scheduler.Option{
		scheduler.WithWorkers(numWorkers),
		scheduler.WithLifecycle(ts),
	}, opts...)
	ts.Scheduler = scheduler.NewScheduler(registry, opts...)
	return ts
}

func testPID() relay.PID {
	return relay.PID{UniqID: "time-test"}
}

func newLuaProcessWithChannels(script string) *engine.Process {
	proto, _ := lua.CompileString(script, "test.lua")
	return engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(engine.BindChannelFunctions),
		engine.WithModuleBinder(timeyields.BindYields),
	)
}

// TestTickerBasic tests basic ticker functionality with channel API
func TestTickerBasic(t *testing.T) {
	sched := newTestScheduler(4)
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
	sched := newTestScheduler(4)
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
	sched := newTestScheduler(4)
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
