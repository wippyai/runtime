package engine

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	lua "github.com/yuin/gopher-lua"
)

// TestAllocProfile traces every allocation in process creation
func TestAllocProfile(t *testing.T) {
	script := `return 1`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	opts := lua.Options{
		RegistrySize:        128,
		RegistryMaxSize:     256 * 256,
		RegistryGrowStep:    16,
		SkipOpenLibs:        true,
		CallStackSize:       128,
		MinimizeStackMemory: true,
	}

	// Warmup - ensure all sync.Once have fired
	warmupCtx, _ := ctxapi.OpenFrameContext(context.Background())
	warmupProc := NewProcess(WithProto(proto))
	warmupProc.Init(warmupCtx, "", nil)
	warmupProc.Close()

	// Extra warmup for OpenBase
	wl := lua.NewState(opts)
	wl.Push(wl.NewFunction(lua.OpenBase))
	wl.Push(lua.LString(lua.BaseLibName))
	wl.Call(1, 0)
	wl.Close()

	t.Log("=== After warmup, profiling allocations ===")

	// 1. LState only
	lstateResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := lua.NewState(opts)
			l.Close()
		}
	})
	t.Logf("LState only:          %d allocs, %d bytes", lstateResult.AllocsPerOp(), lstateResult.AllocedBytesPerOp())

	// 2. LState + OpenBase
	openBaseResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := lua.NewState(opts)
			l.Push(l.NewFunction(lua.OpenBase))
			l.Push(lua.LString(lua.BaseLibName))
			l.Call(1, 0)
			l.Close()
		}
	})
	t.Logf("LState + OpenBase:    %d allocs, %d bytes (delta: +%d, +%d)",
		openBaseResult.AllocsPerOp(), openBaseResult.AllocedBytesPerOp(),
		openBaseResult.AllocsPerOp()-lstateResult.AllocsPerOp(),
		openBaseResult.AllocedBytesPerOp()-lstateResult.AllocedBytesPerOp())

	// 3. LState + OpenBase + native libs
	nativeResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := lua.NewState(opts)
			l.Push(l.NewFunction(lua.OpenBase))
			l.Push(lua.LString(lua.BaseLibName))
			l.Call(1, 0)
			lua.OpenTable(l)
			lua.OpenString(l)
			lua.OpenMath(l)
			lua.OpenCoroutine(l)
			l.Close()
		}
	})
	t.Logf("+ 4 native libs:      %d allocs, %d bytes (delta: +%d, +%d)",
		nativeResult.AllocsPerOp(), nativeResult.AllocedBytesPerOp(),
		nativeResult.AllocsPerOp()-openBaseResult.AllocsPerOp(),
		nativeResult.AllocedBytesPerOp()-openBaseResult.AllocedBytesPerOp())

	// 4. + core modules
	coreResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := lua.NewState(opts)
			l.Push(l.NewFunction(lua.OpenBase))
			l.Push(lua.LString(lua.BaseLibName))
			l.Call(1, 0)
			lua.OpenTable(l)
			lua.OpenString(l)
			lua.OpenMath(l)
			lua.OpenCoroutine(l)
			LoadCoreModules(l)
			l.Close()
		}
	})
	t.Logf("+ core modules:       %d allocs, %d bytes (delta: +%d, +%d)",
		coreResult.AllocsPerOp(), coreResult.AllocedBytesPerOp(),
		coreResult.AllocsPerOp()-nativeResult.AllocsPerOp(),
		nativeResult.AllocedBytesPerOp()-nativeResult.AllocedBytesPerOp())

	// 5. Process struct only (no LState)
	structResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			p := &Process{factory: &Factory{}}
			p.threads = make([]*Task, 0, 4)
			p.queue = NewTaskQueue()
			p.yieldBuf = make([]*Task, 0, 4)
			p.externalTasks = make([]*Task, 0, 8)
			p.outTasks = make([]*Task, 0, 8)
			_ = p
		}
	})
	t.Logf("Process struct only:  %d allocs, %d bytes", structResult.AllocsPerOp(), structResult.AllocedBytesPerOp())

	// 6. Context
	ctxResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx, _ := ctxapi.OpenFrameContext(context.Background())
			_ = ctx
		}
	})
	t.Logf("OpenFrameContext:     %d allocs, %d bytes", ctxResult.AllocsPerOp(), ctxResult.AllocedBytesPerOp())

	// 7. + OpenRestrictedPackage
	pkgResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := lua.NewState(opts)
			l.Push(l.NewFunction(lua.OpenBase))
			l.Push(lua.LString(lua.BaseLibName))
			l.Call(1, 0)
			lua.OpenTable(l)
			lua.OpenString(l)
			lua.OpenMath(l)
			lua.OpenCoroutine(l)
			LoadCoreModules(l)
			l.Push(lua.LGoFunc(OpenRestrictedPackage))
			l.Push(lua.LString(lua.LoadLibName))
			l.Call(1, 0)
			l.Close()
		}
	})
	t.Logf("+ RestrictedPackage:  %d allocs, %d bytes (delta: +%d, +%d)",
		pkgResult.AllocsPerOp(), pkgResult.AllocedBytesPerOp(),
		pkgResult.AllocsPerOp()-coreResult.AllocsPerOp(),
		pkgResult.AllocedBytesPerOp()-coreResult.AllocedBytesPerOp())

	// 8. Factory.CreateState (complete)
	factory := &Factory{}
	createStateResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := factory.CreateState()
			l.Close()
		}
	})
	t.Logf("Factory.CreateState:  %d allocs, %d bytes", createStateResult.AllocsPerOp(), createStateResult.AllocedBytesPerOp())

	// 9. Full process
	fullResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx, _ := ctxapi.OpenFrameContext(context.Background())
			proc := NewProcess(WithProto(proto))
			proc.Init(ctx, "", nil)
			proc.Close()
		}
	})
	t.Logf("Full process:         %d allocs, %d bytes", fullResult.AllocsPerOp(), fullResult.AllocedBytesPerOp())

	t.Log("\n=== Summary ===")
	t.Logf("LState base:          %d allocs, %d bytes", lstateResult.AllocsPerOp(), lstateResult.AllocedBytesPerOp())
	t.Logf("OpenBase overhead:    %d allocs, %d bytes",
		openBaseResult.AllocsPerOp()-lstateResult.AllocsPerOp(),
		openBaseResult.AllocedBytesPerOp()-lstateResult.AllocedBytesPerOp())
	t.Logf("Native libs overhead: %d allocs, %d bytes",
		nativeResult.AllocsPerOp()-openBaseResult.AllocsPerOp(),
		nativeResult.AllocedBytesPerOp()-openBaseResult.AllocedBytesPerOp())
	t.Logf("Core mods overhead:   %d allocs, %d bytes",
		coreResult.AllocsPerOp()-nativeResult.AllocsPerOp(),
		coreResult.AllocedBytesPerOp()-nativeResult.AllocedBytesPerOp())
	t.Logf("Process struct:       %d allocs, %d bytes", structResult.AllocsPerOp(), structResult.AllocedBytesPerOp())
	t.Logf("Context:              %d allocs, %d bytes", ctxResult.AllocsPerOp(), ctxResult.AllocedBytesPerOp())
}
