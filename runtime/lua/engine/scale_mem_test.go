package engine

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	lua "github.com/yuin/gopher-lua"
)

// TestProcessMemoryAtScale measures actual heap delta when creating many processes
func TestProcessMemoryAtScale(t *testing.T) {
	script := `return 1`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	// Warmup - ensure all singletons, sync.Once, etc are initialized
	for i := 0; i < 100; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithProto(proto))
		proc.Init(ctx, "", nil)
		proc.Close()
	}
	runtime.GC()
	runtime.GC()

	counts := []int{1000, 10000, 100000}
	if testing.Short() {
		counts = []int{1000}
	}

	for _, count := range counts {
		t.Run(formatCount(count), func(t *testing.T) {
			// Force GC and get baseline
			runtime.GC()
			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)

			// Create N processes and keep them alive
			procs := make([]*Process, count)
			ctxs := make([]context.Context, count)
			for i := 0; i < count; i++ {
				ctx, _ := ctxapi.OpenFrameContext(context.Background())
				proc := NewProcess(WithProto(proto))
				proc.Init(ctx, "", nil)
				procs[i] = proc
				ctxs[i] = ctx
			}

			// Measure heap with all processes alive
			runtime.GC()
			var after runtime.MemStats
			runtime.ReadMemStats(&after)

			heapDelta := after.HeapAlloc - before.HeapAlloc
			perProcess := heapDelta / uint64(count)

			t.Logf("Processes:     %d", count)
			t.Logf("Heap before:   %d KB", before.HeapAlloc/1024)
			t.Logf("Heap after:    %d KB", after.HeapAlloc/1024)
			t.Logf("Heap delta:    %d KB", heapDelta/1024)
			t.Logf("Per process:   %d bytes", perProcess)

			// Cleanup
			for _, p := range procs {
				p.Close()
			}
		})
	}
}

// TestLStateOnlyAtScale measures just LState memory overhead
func TestLStateOnlyAtScale(t *testing.T) {
	opts := lua.Options{
		RegistrySize:        128,
		RegistryMaxSize:     256 * 256,
		RegistryGrowStep:    16,
		SkipOpenLibs:        true,
		CallStackSize:       128,
		MinimizeStackMemory: true,
	}

	// Warmup
	for i := 0; i < 100; i++ {
		l := lua.NewState(opts)
		l.Close()
	}
	runtime.GC()
	runtime.GC()

	counts := []int{1000, 10000}
	if testing.Short() {
		counts = []int{1000}
	}

	for _, count := range counts {
		t.Run(formatCount(count), func(t *testing.T) {
			runtime.GC()
			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)

			// Create N LStates and keep them alive
			states := make([]*lua.LState, count)
			for i := 0; i < count; i++ {
				states[i] = lua.NewState(opts)
			}

			runtime.GC()
			var after runtime.MemStats
			runtime.ReadMemStats(&after)

			heapDelta := after.HeapAlloc - before.HeapAlloc
			perState := heapDelta / uint64(count)

			t.Logf("LStates:       %d", count)
			t.Logf("Heap delta:    %d KB", heapDelta/1024)
			t.Logf("Per LState:    %d bytes", perState)

			for _, l := range states {
				l.Close()
			}
		})
	}
}

// TestLStateWithLibsAtScale measures LState + cached libs
func TestLStateWithLibsAtScale(t *testing.T) {
	opts := lua.Options{
		RegistrySize:        128,
		RegistryMaxSize:     256 * 256,
		RegistryGrowStep:    16,
		SkipOpenLibs:        true,
		CallStackSize:       128,
		MinimizeStackMemory: true,
	}

	// Warmup - ensure cached libs are built
	for i := 0; i < 100; i++ {
		l := lua.NewState(opts)
		lua.OpenBase(l)
		BindCachedLibs(l)
		l.Close()
	}
	runtime.GC()
	runtime.GC()

	counts := []int{1000, 10000}
	if testing.Short() {
		counts = []int{1000}
	}

	for _, count := range counts {
		t.Run(formatCount(count), func(t *testing.T) {
			runtime.GC()
			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)

			states := make([]*lua.LState, count)
			for i := 0; i < count; i++ {
				l := lua.NewState(opts)
				lua.OpenBase(l)
				BindCachedLibs(l)
				states[i] = l
			}

			runtime.GC()
			var after runtime.MemStats
			runtime.ReadMemStats(&after)

			heapDelta := after.HeapAlloc - before.HeapAlloc
			perState := heapDelta / uint64(count)

			t.Logf("LStates+cached: %d", count)
			t.Logf("Heap delta:     %d KB", heapDelta/1024)
			t.Logf("Per LState:     %d bytes", perState)

			for _, l := range states {
				l.Close()
			}
		})
	}
}

func formatCount(n int) string {
	switch {
	case n >= 1000000:
		return fmt.Sprintf("%dM", n/1000000)
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// TestMessageThroughput measures message send/receive rate
func TestMessageThroughput(t *testing.T) {
	script := `
		local ch = channel.new(1000)
		subscribe("msg", ch)

		local count = 0
		while true do
			ch:receive()
			count = count + 1
		end
	`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(WithProto(proto))
	proc.Init(ctx, "", nil)
	LoadModuleDef(proc.State(), ChannelModule)
	loadPubSubGlobals(proc.State())
	defer proc.Close()

	// Run until idle (waiting for message)
	var output process.StepOutput
	for i := 0; i < 100; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatal(err)
		}
		if output.Status() == process.StepIdle {
			break
		}
	}

	counts := []int{10000, 100000}
	if testing.Short() {
		counts = []int{10000}
	}

	for _, count := range counts {
		t.Run(formatCount(count), func(t *testing.T) {
			start := time.Now()
			for i := 0; i < count; i++ {
				events := []process.Event{{
					Type: process.EventMessage,
					Data: &relay.Package{
						Messages: []*relay.Message{{Topic: "msg", Payloads: payload.Payloads{payload.NewString("test")}}},
					},
				}}
				output.Reset()
				if err := proc.Step(events, &output); err != nil {
					t.Fatal(err)
				}
			}
			elapsed := time.Since(start)
			rate := float64(count) / elapsed.Seconds()
			t.Logf("Sent %d messages in %v (%.0f/sec, %.2fμs each)", count, elapsed, rate, float64(elapsed.Microseconds())/float64(count))
		})
	}
}

// TestSpawnThroughput measures process spawn rate with pooling
func TestSpawnThroughput(t *testing.T) {
	script := `return 1`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	// Warmup
	for i := 0; i < 1000; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithProto(proto))
		proc.Init(ctx, "", nil)
		proc.Close()
	}
	runtime.GC()

	counts := []int{10000, 100000}
	if testing.Short() {
		counts = []int{10000}
	}

	for _, count := range counts {
		t.Run(formatCount(count), func(t *testing.T) {
			start := time.Now()
			for i := 0; i < count; i++ {
				ctx, _ := ctxapi.OpenFrameContext(context.Background())
				proc := NewProcess(WithProto(proto))
				proc.Init(ctx, "", nil)
				proc.Close()
			}
			elapsed := time.Since(start)
			rate := float64(count) / elapsed.Seconds()
			t.Logf("Spawned %d processes in %v (%.0f/sec, %.2fμs each)", count, elapsed, rate, float64(elapsed.Microseconds())/float64(count))
		})
	}
}
