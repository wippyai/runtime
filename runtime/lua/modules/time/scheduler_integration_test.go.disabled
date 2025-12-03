package time

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	apiruntime "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/system/clock"
	scheduler "github.com/wippyai/runtime/system/scheduler/actor"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	lua "github.com/yuin/gopher-lua"
)

type testScheduler struct {
	*scheduler.Scheduler
	clock *clock.Dispatcher
}

func (ts *testScheduler) Stop() {
	ts.Scheduler.Stop()
	if ts.clock != nil {
		ts.clock.Stop(context.Background())
	}
}

func newTestScheduler(numWorkers int, opts ...scheduler.Option) *testScheduler {
	registry := scheduler.NewRegistry()
	clockSvc := clock.NewDispatcher()
	clockSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		registry.Register(id, h)
	})
	opts = append([]scheduler.Option{scheduler.WithWorkers(numWorkers)}, opts...)
	return &testScheduler{
		Scheduler: scheduler.NewScheduler(registry, opts...),
		clock:     clockSvc,
	}
}

func testPID() relay.PID {
	return relay.PID{UniqID: "test"}
}

func newLuaProcess(script string) *engine.Process {
	proto, _ := lua.CompileString(script, "test.lua")
	return engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(BindYields),
	)
}

// TestSchedulerWithLuaSleep tests Lua process with time.sleep via real scheduler
func TestSchedulerWithLuaSleep(t *testing.T) {
	sched := newTestScheduler(4)
	sched.Start()
	defer sched.Stop()

	script := `
		time.sleep(50 * time.MILLISECOND)
		return "done"
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcess(script)

	start := time.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	if elapsed < 50*time.Millisecond {
		t.Fatalf("sleep too short: %v", elapsed)
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("sleep too long: %v", elapsed)
	}
	t.Logf("Single 50ms sleep completed in %v", elapsed)
}

// TestSchedulerWithSpawnNoSleep tests Lua spawn (no sleep in children) via real scheduler
func TestSchedulerWithSpawnNoSleep(t *testing.T) {
	sched := newTestScheduler(4)
	sched.Start()
	defer sched.Stop()

	script := `
		local results = {}

		-- Spawn 5 coroutines that compute (no sleep)
		for i = 1, 5 do
			coroutine.spawn(function()
				local sum = 0
				for j = 1, 100 do
					sum = sum + j
				end
				results[i] = sum
			end)
		end

		-- Yield to let spawns execute
		for i = 1, 10 do
			coroutine.yield()
		end

		-- Count completed (sum of 1..100 = 5050)
		local count = 0
		for i = 1, 5 do
			if results[i] == 5050 then
				count = count + 1
			end
		end
		return count
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcess(script)

	start := time.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	t.Logf("Spawn (no sleep) completed in %v", elapsed)
}

// TestParallelLuaSleep tests parallel Lua processes with sleep
func TestParallelLuaSleep(t *testing.T) {
	var completed atomic.Int32

	sched := newTestScheduler(runtime.GOMAXPROCS(0),
		scheduler.WithOnComplete(func(ctx context.Context, pid relay.PID, result *apiruntime.Result) {
			completed.Add(1)
		}),
	)
	sched.Start()
	defer sched.Stop()

	const n = 100
	script := `
		time.sleep(10 * time.MILLISECOND)
		return "done"
	`

	start := time.Now()

	// Submit n parallel Lua processes
	for i := 0; i < n; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := newLuaProcess(script)
		sched.Submit(ctx, testPID(), proc, "", nil)
	}

	// Wait for completion
	deadline := time.Now().Add(5 * time.Second)
	for completed.Load() < int32(n) && time.Now().Before(deadline) {
		runtime.Gosched()
	}

	elapsed := time.Since(start)

	if completed.Load() != int32(n) {
		t.Fatalf("expected %d completed, got %d", n, completed.Load())
	}

	// All 100 sleeps should complete in parallel
	if elapsed > 500*time.Millisecond {
		t.Fatalf("parallel sleep took too long: %v", elapsed)
	}
	t.Logf("100 parallel Lua 10ms sleeps completed in %v", elapsed)
}

// TestManySpawnedCoroutines tests many spawned coroutines per process
func TestManySpawnedCoroutines(t *testing.T) {
	sched := newTestScheduler(4)
	sched.Start()
	defer sched.Stop()

	script := `
		local count = 0

		-- Spawn 100 coroutines
		for i = 1, 100 do
			coroutine.spawn(function()
				local sum = 0
				for j = 1, 100 do
					sum = sum + j
				end
				count = count + 1
			end)
		end

		-- Yield to let spawns complete
		for i = 1, 200 do
			coroutine.yield()
		end

		return count
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcess(script)

	start := time.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	t.Logf("100 spawned coroutines completed in %v", elapsed)
}

// BenchmarkLuaProcessWithSleep benchmarks Lua process with real sleep
func BenchmarkLuaProcessWithSleep(b *testing.B) {
	sched := newTestScheduler(runtime.GOMAXPROCS(0))
	sched.Start()
	defer sched.Stop()

	script := `
		time.sleep(time.MICROSECOND)
		return 1
	`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := newLuaProcess(script)
		sched.Execute(ctx, testPID(), proc, "", nil)
	}
}

// BenchmarkLuaProcessWithSpawn benchmarks Lua process with spawn
func BenchmarkLuaProcessWithSpawn(b *testing.B) {
	sched := newTestScheduler(runtime.GOMAXPROCS(0))
	sched.Start()
	defer sched.Stop()

	script := `
		for i = 1, 10 do
			coroutine.spawn(function()
				local sum = 0
				for j = 1, 10 do
					sum = sum + j
				end
			end)
		end
		for i = 1, 20 do
			coroutine.yield()
		end
		return 1
	`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := newLuaProcess(script)
		sched.Execute(ctx, testPID(), proc, "", nil)
	}
}

// BenchmarkLuaProcessParallel benchmarks parallel Lua processes
func BenchmarkLuaProcessParallel(b *testing.B) {
	sched := newTestScheduler(runtime.GOMAXPROCS(0))
	sched.Start()
	defer sched.Stop()

	script := `
		time.sleep(time.MICROSECOND)
		return 1
	`

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx, _ := ctxapi.OpenFrameContext(context.Background())
			proc := newLuaProcess(script)
			sched.Execute(ctx, testPID(), proc, "", nil)
		}
	})
}

// BenchmarkSpawnThroughput benchmarks spawning throughput
func BenchmarkSpawnThroughput(b *testing.B) {
	sched := newTestScheduler(runtime.GOMAXPROCS(0))
	sched.Start()
	defer sched.Stop()

	script := `
		for i = 1, 100 do
			coroutine.spawn(function()
				return i
			end)
		end
		for i = 1, 150 do
			coroutine.yield()
		end
		return 1
	`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := newLuaProcess(script)
		sched.Execute(ctx, testPID(), proc, "", nil)
	}
	b.ReportMetric(float64(b.N*100), "spawns")
}

// newLuaProcessWithChannels creates process with channel support
func newLuaProcessWithChannels(script string) *engine.Process {
	proto, _ := lua.CompileString(script, "test.lua")
	return engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(Bind),
		engine.WithModuleBinder(engine.BindChannelFunctions),
	)
}

// TestActorStyleTimerChannel tests actor pattern: spawn workers with timers sending to channels
func TestActorStyleTimerChannel(t *testing.T) {
	sched := newTestScheduler(4)
	sched.Start()
	defer sched.Stop()

	script := `
		local results = channel.new(10)
		local done = channel.new(1)
		local numWorkers = 5
		local sleepMs = 5

		for i = 1, numWorkers do
			coroutine.spawn(function()
				time.sleep(sleepMs * time.MILLISECOND)
				results:send(i * 10)
			end)
		end

		coroutine.spawn(function()
			local sum = 0
			for i = 1, numWorkers do
				local v = results:receive()
				sum = sum + v
			end
			done:send(sum)
		end)

		local total = done:receive()
		return total
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcessWithChannels(script)

	start := time.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	t.Logf("Actor-style timer+channel completed in %v", elapsed)
}

// TestWorkerPoolPattern tests worker pool with timer delays and channel coordination
func TestWorkerPoolPattern(t *testing.T) {
	sched := newTestScheduler(4)
	sched.Start()
	defer sched.Stop()

	script := `
		local jobs = channel.new(10)
		local results = channel.new(10)
		local numWorkers = 3
		local numJobs = 6

		for w = 1, numWorkers do
			coroutine.spawn(function()
				while true do
					local job, ok = jobs:receive()
					if not ok then break end
					time.sleep(time.MILLISECOND)
					results:send(job * 2)
				end
			end)
		end

		for j = 1, numJobs do
			jobs:send(j)
		end
		jobs:close()

		local sum = 0
		for i = 1, numJobs do
			local r = results:receive()
			sum = sum + r
		end

		return sum
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcessWithChannels(script)

	start := time.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	t.Logf("Worker pool pattern completed in %v", elapsed)
}

// TestTimeoutSelectPattern tests timeout pattern with select and timer
func TestTimeoutSelectPattern(t *testing.T) {
	sched := newTestScheduler(4)
	sched.Start()
	defer sched.Stop()

	script := `
		local data = channel.new(0)
		local result = nil

		coroutine.spawn(function()
			time.sleep(20 * time.MILLISECOND)
			data:send("data arrived")
		end)

		local idx = channel.select(data:case_receive(), nil)
		if idx == 0 then
			result = "default"
		else
			result = "got data"
		end

		return result
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcessWithChannels(script)

	start := time.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	t.Logf("Timeout select pattern completed in %v", elapsed)
}

// TestPipelinePattern tests pipeline of workers with timers
func TestPipelinePattern(t *testing.T) {
	sched := newTestScheduler(4)
	sched.Start()
	defer sched.Stop()

	script := `
		local stage1 = channel.new(5)
		local stage2 = channel.new(5)
		local stage3 = channel.new(5)

		coroutine.spawn(function()
			for i = 1, 5 do
				time.sleep(time.MILLISECOND)
				stage1:send(i)
			end
			stage1:close()
		end)

		coroutine.spawn(function()
			while true do
				local v, ok = stage1:receive()
				if not ok then break end
				stage2:send(v * 2)
			end
			stage2:close()
		end)

		coroutine.spawn(function()
			while true do
				local v, ok = stage2:receive()
				if not ok then break end
				stage3:send(v + 1)
			end
			stage3:close()
		end)

		local sum = 0
		while true do
			local v, ok = stage3:receive()
			if not ok then break end
			sum = sum + v
		end

		return sum
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcessWithChannels(script)

	start := time.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	t.Logf("Pipeline pattern completed in %v", elapsed)
}

// BenchmarkActorTimerChannel benchmarks actor pattern with timer+channel
func BenchmarkActorTimerChannel(b *testing.B) {
	sched := newTestScheduler(runtime.GOMAXPROCS(0))
	sched.Start()
	defer sched.Stop()

	script := `
		local results = channel.new(5)

		for i = 1, 5 do
			coroutine.spawn(function()
				time.sleep(time.MICROSECOND)
				results:send(i)
			end)
		end

		local sum = 0
		for i = 1, 5 do
			sum = sum + results:receive()
		end
		return sum
	`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := newLuaProcessWithChannels(script)
		sched.Execute(ctx, testPID(), proc, "", nil)
	}
}

// BenchmarkChannelOnlyNoTimer benchmarks channel without timer overhead
func BenchmarkChannelOnlyNoTimer(b *testing.B) {
	sched := newTestScheduler(runtime.GOMAXPROCS(0))
	sched.Start()
	defer sched.Stop()

	script := `
		local ch = channel.new(10)

		for i = 1, 10 do
			coroutine.spawn(function()
				ch:send(i)
			end)
		end

		for i = 1, 20 do coroutine.yield() end

		local sum = 0
		for i = 1, 10 do
			sum = sum + ch:receive()
		end
		return sum
	`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := newLuaProcessWithChannels(script)
		sched.Execute(ctx, testPID(), proc, "", nil)
	}
}

// BenchmarkPipelinePattern benchmarks pipeline with timers
func BenchmarkPipelinePattern(b *testing.B) {
	sched := newTestScheduler(runtime.GOMAXPROCS(0))
	sched.Start()
	defer sched.Stop()

	script := `
		local stage1 = channel.new(3)
		local stage2 = channel.new(3)

		coroutine.spawn(function()
			for i = 1, 3 do
				time.sleep(time.MICROSECOND)
				stage1:send(i)
			end
			stage1:close()
		end)

		coroutine.spawn(function()
			while true do
				local v, ok = stage1:receive()
				if not ok then break end
				stage2:send(v * 2)
			end
			stage2:close()
		end)

		local sum = 0
		while true do
			local v, ok = stage2:receive()
			if not ok then break end
			sum = sum + v
		end
		return sum
	`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := newLuaProcessWithChannels(script)
		sched.Execute(ctx, testPID(), proc, "", nil)
	}
}

// TestConcurrentCoroutineYields tests that multiple coroutines yielding in the same step
// get their results routed correctly.
func TestConcurrentCoroutineYields(t *testing.T) {
	sched := newTestScheduler(4)
	sched.Start()
	defer sched.Stop()

	// Test pattern: spawn 5 coroutines with different sleep times
	// Each records arrival order - shorter sleeps should complete first
	script := `
		local results = channel.new(10)
		local order = {}
		local orderIdx = 0

		-- Spawn coroutines with varying sleep durations
		for i = 1, 5 do
			coroutine.spawn(function()
				local sleepMs = (6 - i) * 5  -- 25, 20, 15, 10, 5ms
				time.sleep(sleepMs * time.MILLISECOND)
				orderIdx = orderIdx + 1
				order[orderIdx] = i
				results:send(i)
			end)
		end

		-- Wait for all results
		local sum = 0
		for j = 1, 5 do
			sum = sum + results:receive()
		end

		-- Verify completion order (smaller sleeps should finish first)
		-- Coroutine 5 sleeps 5ms, 4 sleeps 10ms, etc.
		-- So expected order: 5, 4, 3, 2, 1
		local orderOk = true
		for idx, expected in ipairs({5, 4, 3, 2, 1}) do
			if order[idx] ~= expected then
				orderOk = false
			end
		end

		if orderOk then
			return sum  -- 1+2+3+4+5 = 15
		else
			return -1
		end
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcessWithChannels(script)

	start := time.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	t.Logf("Concurrent coroutine yields completed in %v", elapsed)
}

// TestManyParallelYields tests that more than 4 concurrent yields (overflow case) work
func TestManyParallelYields(t *testing.T) {
	sched := newTestScheduler(8)
	sched.Start()
	defer sched.Stop()

	// Spawn 8 coroutines (exceeds the 4-slot fixed buffer)
	script := `
		local results = channel.new(20)
		local count = 8

		for i = 1, count do
			coroutine.spawn(function()
				time.sleep(5 * time.MILLISECOND)
				results:send(i)
			end)
		end

		local sum = 0
		for j = 1, count do
			sum = sum + results:receive()
		end

		return sum
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcessWithChannels(script)

	start := time.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	// Sum should be 1+2+3+4+5+6+7+8 = 36
	t.Logf("Many parallel yields (8 workers) completed in %v", elapsed)
}

// TestTimerBasic tests basic timer functionality with scheduler
func TestTimerBasic(t *testing.T) {
	sched := newTestScheduler(4)
	sched.Start()
	defer sched.Stop()

	script := `
		local timer = time.timer(20 * time.MILLISECOND)
		local ch = timer:channel()
		local fireTime = ch:receive()
		return fireTime > 0
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcess(script)

	start := time.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	if elapsed < 20*time.Millisecond {
		t.Fatalf("timer too short: %v", elapsed)
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("timer too long: %v", elapsed)
	}

	t.Logf("Timer basic: completed in %v", elapsed)
}

// TestTimerStop tests stopping a timer before it fires
func TestTimerStop(t *testing.T) {
	sched := newTestScheduler(4)
	sched.Start()
	defer sched.Stop()

	script := `
		local timer = time.timer(time.SECOND)
		local stopped = timer:stop()
		return stopped
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcess(script)

	start := time.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	if elapsed > 100*time.Millisecond {
		t.Fatalf("stop took too long: %v", elapsed)
	}

	t.Logf("Timer stop: completed in %v", elapsed)
}

// TestTimerMultiple tests multiple timers with different durations
func TestTimerMultiple(t *testing.T) {
	sched := newTestScheduler(4)
	sched.Start()
	defer sched.Stop()

	script := `
		local results = {}

		local timer1 = time.timer(10 * time.MILLISECOND)
		local timer2 = time.timer(20 * time.MILLISECOND)

		local ch1 = timer1:channel()
		local ch2 = timer2:channel()

		results[1] = ch1:receive()
		results[2] = ch2:receive()

		return results[1] > 0 and results[2] > 0 and results[2] >= results[1]
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcess(script)

	start := time.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	if elapsed < 20*time.Millisecond {
		t.Fatalf("multiple timers too short: %v", elapsed)
	}

	t.Logf("Timer multiple: completed in %v", elapsed)
}

// TestTimerWithSpawn tests timer in spawned coroutine
func TestTimerWithSpawn(t *testing.T) {
	sched := newTestScheduler(4)
	sched.Start()
	defer sched.Stop()

	script := `
		local done = channel.new(0)

		coroutine.spawn(function()
			local timer = time.timer(10 * time.MILLISECOND)
			local ch = timer:channel()
			local fireTime = ch:receive()
			done:send(fireTime)
		end)

		local result = done:receive()
		return result > 0
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := newLuaProcessWithChannels(script)

	start := time.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	if elapsed < 10*time.Millisecond {
		t.Fatalf("timer with spawn too short: %v", elapsed)
	}

	t.Logf("Timer with spawn: completed in %v", elapsed)
}

// Note: TestTimeReferenceWithNow requires context propagation to LState which
// is not currently implemented in engine. The TimeReference integration is tested
// in service/dispatcher/clock/handler_test.go at the handler level.
// When context propagation is added, a scheduler integration test can be added here.

// Placeholder to verify clockapi.TimeReference exists and interface is correct
func TestTimeReferenceInterface(t *testing.T) {
	fixedTime := time.Date(2020, 6, 15, 10, 30, 0, 0, time.UTC)
	mockRef := &mockTimeRef{now: fixedTime}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	err := clockapi.WithTimeReference(ctx, mockRef)
	if err != nil {
		t.Fatalf("failed to set time reference: %v", err)
	}

	ref := clockapi.GetTimeReference(ctx)
	if ref == nil {
		t.Fatal("failed to get time reference")
	}

	if !ref.Now().Equal(fixedTime) {
		t.Errorf("expected %v, got %v", fixedTime, ref.Now())
	}

	t.Logf("TimeReference interface works correctly with FrameContext")
}

type mockTimeRef struct {
	now time.Time
}

func (m *mockTimeRef) Now() time.Time       { return m.now }
func (m *mockTimeRef) StartTime() time.Time { return m.now }

// TestFuncPoolWithSleep tests that sleep blocks properly when using Static pool (funcpool)
// This mirrors the HTTP endpoint execution path which uses Static pool + Executor
func TestFuncPoolWithSleep(t *testing.T) {
	// Create dispatcher that registers clock handlers
	type poolDispatcher struct {
		handlers map[dispatcher.CommandID]dispatcher.Handler
	}
	disp := &poolDispatcher{handlers: make(map[dispatcher.CommandID]dispatcher.Handler)}
	clockSvc := clock.NewDispatcher()
	clockSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		disp.handlers[id] = h
	})

	dispatcherImpl := dispatcherFunc(func(cmd dispatcher.Command) dispatcher.Handler {
		return disp.handlers[cmd.CmdID()]
	})

	factory := func() (process.Process, error) {
		script := `
			time.sleep(50 * time.MILLISECOND)
			return "done"
		`
		return newLuaProcess(script), nil
	}

	pool, err := funcpool.NewStatic(factory, dispatcherImpl, funcpool.Config{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}

	pool.Start()
	defer pool.Stop()

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	start := time.Now()
	result, err := pool.Call(ctx, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if result != nil && result.Error != nil {
		t.Fatalf("Result error: %v", result.Error)
	}

	t.Logf("FuncPool sleep took: %v", elapsed)

	if elapsed < 50*time.Millisecond {
		t.Fatalf("sleep too short: %v (expected >= 50ms)", elapsed)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("sleep too long: %v", elapsed)
	}
}

type dispatcherFunc func(cmd dispatcher.Command) dispatcher.Handler

func (f dispatcherFunc) Dispatch(cmd dispatcher.Command) dispatcher.Handler {
	return f(cmd)
}

// TestWorkStealingPoolWithSleep tests that sleep blocks properly in work-stealing pool
// This matches the production HTTP endpoint configuration
func TestWorkStealingPoolWithSleep(t *testing.T) {
	// Create dispatcher that registers clock handlers
	type poolDispatcher struct {
		handlers map[dispatcher.CommandID]dispatcher.Handler
	}
	disp := &poolDispatcher{handlers: make(map[dispatcher.CommandID]dispatcher.Handler)}
	clockSvc := clock.NewDispatcher()
	clockSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		disp.handlers[id] = h
	})

	dispatcherImpl := dispatcherFunc(func(cmd dispatcher.Command) dispatcher.Handler {
		return disp.handlers[cmd.CmdID()]
	})

	factory := func() (process.Process, error) {
		script := `
			time.sleep(50 * time.MILLISECOND)
			return "done"
		`
		return newLuaProcess(script), nil
	}

	pool, err := funcpool.NewWorkStealing(factory, dispatcherImpl, funcpool.WorkStealingConfig{
		Workers:   4,
		QueueSize: 64,
	})
	if err != nil {
		t.Fatal(err)
	}

	pool.Start()
	defer pool.Stop()

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	start := time.Now()
	result, err := pool.Call(ctx, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if result != nil && result.Error != nil {
		t.Fatalf("Result error: %v", result.Error)
	}

	t.Logf("WorkStealing pool sleep took: %v", elapsed)

	if elapsed < 50*time.Millisecond {
		t.Fatalf("sleep too short: %v (expected >= 50ms)", elapsed)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("sleep too long: %v", elapsed)
	}
}

// BenchmarkSelectWithTimer benchmarks select operations with timer fallback
func BenchmarkSelectWithTimer(b *testing.B) {
	sched := newTestScheduler(runtime.GOMAXPROCS(0))
	sched.Start()
	defer sched.Stop()

	script := `
		local ch1 = channel.new(1)
		local ch2 = channel.new(1)
		ch1:send(1)

		for i = 1, 10 do
			local idx = channel.select(ch1:case_receive(), ch2:case_receive(), nil)
			if idx ~= 0 then
				ch1:send(1)
			end
		end
		return 1
	`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := newLuaProcessWithChannels(script)
		sched.Execute(ctx, testPID(), proc, "", nil)
	}
}
