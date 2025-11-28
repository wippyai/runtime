package engine2

import (
	"context"
	"runtime"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/low-engine-v2/scheduler"
	lua "github.com/yuin/gopher-lua"
)

func setupChannelProc(b *testing.B, script string) *Process {
	proto, _ := lua.CompileString(script, "bench.lua")
	proc := NewProcess(
		WithProto(proto),
		WithLayer(NewChannelLayer()),
		WithModuleBinder(BindTimeSleep),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Start(ctx, nil); err != nil {
		b.Fatal(err)
	}

	BindChannelFunctions(proc.State(), proc)
	return proc
}

func runProcToCompletion(b *testing.B, proc *Process, maxSteps int) {
	for i := 0; i < maxSteps; i++ {
		result, err := proc.Step(nil)
		if err != nil {
			b.Fatal(err)
		}
		if result.Status == scheduler.StepDone {
			return
		}
	}
	b.Fatal("did not complete")
}

// BenchmarkChannelCreate measures channel creation overhead
func BenchmarkChannelCreate(b *testing.B) {
	script := `
		for i = 1, 1000 do
			local ch = channel.new(0)
		end
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 100)
		proc.Close()
	}
}

// BenchmarkChannelBufferedSendRecv measures buffered send/recv
func BenchmarkChannelBufferedSendRecv(b *testing.B) {
	script := `
		local ch = channel.new(100)
		for i = 1, 100 do
			ch:send(i)
		end
		for i = 1, 100 do
			ch:recv()
		end
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 500)
		proc.Close()
	}
}

// BenchmarkChannelUnbufferedPingPong measures unbuffered coordination
func BenchmarkChannelUnbufferedPingPong(b *testing.B) {
	script := `
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)
		local count = 0

		coroutine.spawn(function()
			for i = 1, 50 do
				ch1:recv()
				ch2:send(true)
			end
		end)

		for i = 1, 50 do
			ch1:send(true)
			ch2:recv()
			count = count + 1
		end
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 500)
		proc.Close()
	}
}

// BenchmarkChannelSelect measures select performance
func BenchmarkChannelSelect(b *testing.B) {
	script := `
		local ch1 = channel.new(1)
		local ch2 = channel.new(1)

		for i = 1, 100 do
			ch1:send(i)
			local idx = channel.select(ch1:case_receive(), ch2:case_receive(), nil)
		end
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 500)
		proc.Close()
	}
}

// BenchmarkChannelMultipleSpawns measures spawn + channel overhead
func BenchmarkChannelMultipleSpawns(b *testing.B) {
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
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 200)
		proc.Close()
	}
}

// BenchmarkChannelProducerConsumer measures producer-consumer pattern
func BenchmarkChannelProducerConsumer(b *testing.B) {
	script := `
		local ch = channel.new(10)
		local done = 0

		coroutine.spawn(function()
			for i = 1, 100 do
				ch:send(i)
			end
			ch:close()
		end)

		coroutine.spawn(function()
			while true do
				local v, ok = ch:recv()
				if not ok then break end
				done = done + 1
			end
		end)

		for i = 1, 200 do coroutine.yield() end
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 500)
		proc.Close()
	}
}

// BenchmarkChannelMemory measures memory per channel operation
func BenchmarkChannelMemory(b *testing.B) {
	script := `
		local ch = channel.new(0)
		coroutine.spawn(function()
			ch:send("value")
		end)
		local v = ch:recv()
	`

	b.ResetTimer()
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 100)
		proc.Close()
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	b.ReportMetric(float64(m2.TotalAlloc-m1.TotalAlloc)/float64(b.N), "bytes/op")
}

// BenchmarkSelectCases measures select with varying case counts
func BenchmarkSelectCases2(b *testing.B) {
	script := `
		local ch1 = channel.new(1)
		local ch2 = channel.new(1)
		ch1:send(1)
		for i = 1, 50 do
			channel.select(ch1:case_receive(), ch2:case_receive())
			ch1:send(1)
		end
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 300)
		proc.Close()
	}
}

// BenchmarkPureChannelOps tests raw Channel.Send/Receive without Lua
func BenchmarkPureChannelOps(b *testing.B) {
	ch := NewChannel(100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 100 buffered sends
		for j := 0; j < 100; j++ {
			r := ch.Send(nil, lua.LNumber(j), nil)
			ReleaseResult(r)
		}
		// 100 receives
		for j := 0; j < 100; j++ {
			r := ch.Receive(nil, nil)
			ReleaseResult(r)
		}
	}
}

// BenchmarkPureChannelSendRecv tests single send/recv pair
func BenchmarkPureChannelSendRecv(b *testing.B) {
	ch := NewChannel(1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := ch.Send(nil, lua.LNumber(i), nil)
		ReleaseResult(r)
		r = ch.Receive(nil, nil)
		ReleaseResult(r)
	}
}

// BenchmarkPureChannelZeroAlloc tests with pre-allocated value
func BenchmarkPureChannelZeroAlloc(b *testing.B) {
	ch := NewChannel(100)
	val := lua.LNumber(42)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100; j++ {
			r := ch.Send(nil, val, nil)
			ReleaseResult(r)
		}
		for j := 0; j < 100; j++ {
			r := ch.Receive(nil, nil)
			ReleaseResult(r)
		}
	}
}

func BenchmarkSelectCases4(b *testing.B) {
	script := `
		local ch1 = channel.new(1)
		local ch2 = channel.new(1)
		local ch3 = channel.new(1)
		local ch4 = channel.new(1)
		ch1:send(1)
		for i = 1, 50 do
			channel.select(ch1:case_receive(), ch2:case_receive(), ch3:case_receive(), ch4:case_receive())
			ch1:send(1)
		end
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 300)
		proc.Close()
	}
}
