package engine2

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/relay"
	apiruntime "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/service/dispatcher/clock"
	scheduler "github.com/wippyai/runtime/system/scheduler/actor"
	lua "github.com/yuin/gopher-lua"
)

func newTestScheduler(numWorkers int, opts ...scheduler.Option) *scheduler.Scheduler {
	registry := scheduler.NewRegistry()
	clockSvc := clock.NewService()
	clockSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		registry.Register(id, h)
	})
	opts = append([]scheduler.Option{scheduler.WithWorkers(numWorkers)}, opts...)
	return scheduler.NewScheduler(registry, opts...)
}

func testPID() relay.PID {
	return relay.PID{UniqID: "test"}
}

func newLuaProcess(script string) *Process {
	proto, _ := lua.CompileString(script, "test.lua")
	return NewProcess(
		WithProto(proto),
		WithModuleBinder(BindTimeSleep),
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
func newLuaProcessWithChannels(script string) *Process {
	proto, _ := lua.CompileString(script, "test.lua")
	var proc *Process
	proc = NewProcess(
		WithProto(proto),
		WithLayer(NewChannelLayer()),
		WithModuleBinder(BindTimeSleep),
		WithModuleBinder(func(l *lua.LState) {
			BindChannelFunctions(l, proc)
		}),
	)
	return proc
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
				local v = results:recv()
				sum = sum + v
			end
			done:send(sum)
		end)

		local total = done:recv()
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
					local job, ok = jobs:recv()
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
			local r = results:recv()
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
				local v, ok = stage1:recv()
				if not ok then break end
				stage2:send(v * 2)
			end
			stage2:close()
		end)

		coroutine.spawn(function()
			while true do
				local v, ok = stage2:recv()
				if not ok then break end
				stage3:send(v + 1)
			end
			stage3:close()
		end)

		local sum = 0
		while true do
			local v, ok = stage3:recv()
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
			sum = sum + results:recv()
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
			sum = sum + ch:recv()
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
				local v, ok = stage1:recv()
				if not ok then break end
				stage2:send(v * 2)
			end
			stage2:close()
		end)

		local sum = 0
		while true do
			local v, ok = stage2:recv()
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
