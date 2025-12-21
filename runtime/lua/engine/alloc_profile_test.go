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

	// Extra warmup for cached libs
	wl := lua.NewState(opts)
	lua.OpenBase(wl)
	BindCachedLibs(wl)
	wl.Close()

	t.Log("=== Allocation breakdown ===")

	// 1. LState only
	lstateResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := lua.NewState(opts)
			l.Close()
		}
	})
	t.Logf("1. LState only:        %d allocs, %d bytes", lstateResult.AllocsPerOp(), lstateResult.AllocedBytesPerOp())

	// 2. + OpenBase
	openBaseResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := lua.NewState(opts)
			lua.OpenBase(l)
			l.Close()
		}
	})
	t.Logf("2. + OpenBase:         %d allocs, %d bytes (delta: +%d, +%d)",
		openBaseResult.AllocsPerOp(), openBaseResult.AllocedBytesPerOp(),
		openBaseResult.AllocsPerOp()-lstateResult.AllocsPerOp(),
		openBaseResult.AllocedBytesPerOp()-lstateResult.AllocedBytesPerOp())

	// 3. + Cached libs (table, string, math, coroutine, errors)
	cachedLibsResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := lua.NewState(opts)
			lua.OpenBase(l)
			BindCachedLibs(l)
			l.Close()
		}
	})
	t.Logf("3. + CachedLibs:       %d allocs, %d bytes (delta: +%d, +%d)",
		cachedLibsResult.AllocsPerOp(), cachedLibsResult.AllocedBytesPerOp(),
		cachedLibsResult.AllocsPerOp()-openBaseResult.AllocsPerOp(),
		cachedLibsResult.AllocedBytesPerOp()-openBaseResult.AllocedBytesPerOp())

	// 4. + RestrictedPackage
	pkgResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := lua.NewState(opts)
			lua.OpenBase(l)
			BindCachedLibs(l)
			l.Push(lua.LGoFunc(OpenRestrictedPackage))
			l.Push(lua.LString(lua.LoadLibName))
			l.Call(1, 0)
			l.Close()
		}
	})
	t.Logf("4. + RestrictedPkg:    %d allocs, %d bytes (delta: +%d, +%d)",
		pkgResult.AllocsPerOp(), pkgResult.AllocedBytesPerOp(),
		pkgResult.AllocsPerOp()-cachedLibsResult.AllocsPerOp(),
		pkgResult.AllocedBytesPerOp()-cachedLibsResult.AllocedBytesPerOp())

	// 5. Factory.CreateState (complete state creation)
	factory := &Factory{}
	createStateResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := factory.CreateState()
			l.Close()
		}
	})
	t.Logf("5. Factory.CreateState: %d allocs, %d bytes", createStateResult.AllocsPerOp(), createStateResult.AllocedBytesPerOp())

	// 6. Process struct only
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
	t.Logf("6. Process struct:      %d allocs, %d bytes", structResult.AllocsPerOp(), structResult.AllocedBytesPerOp())

	// 7. Context
	ctxResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx, _ := ctxapi.OpenFrameContext(context.Background())
			_ = ctx
		}
	})
	t.Logf("7. OpenFrameContext:    %d allocs, %d bytes", ctxResult.AllocsPerOp(), ctxResult.AllocedBytesPerOp())

	// 8. Full process
	fullResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ctx, _ := ctxapi.OpenFrameContext(context.Background())
			proc := NewProcess(WithProto(proto))
			proc.Init(ctx, "", nil)
			proc.Close()
		}
	})
	t.Logf("8. Full process:        %d allocs, %d bytes", fullResult.AllocsPerOp(), fullResult.AllocedBytesPerOp())

	t.Log("\n=== Summary ===")
	t.Logf("LState base:     %5d bytes", lstateResult.AllocedBytesPerOp())
	t.Logf("+ OpenBase:      %5d bytes", openBaseResult.AllocedBytesPerOp()-lstateResult.AllocedBytesPerOp())
	t.Logf("+ CachedLibs:    %5d bytes", cachedLibsResult.AllocedBytesPerOp()-openBaseResult.AllocedBytesPerOp())
	t.Logf("+ RestrictedPkg: %5d bytes", pkgResult.AllocedBytesPerOp()-cachedLibsResult.AllocedBytesPerOp())
	t.Logf("+ Process struct:%5d bytes", structResult.AllocedBytesPerOp())
	t.Logf("+ Context:       %5d bytes", ctxResult.AllocedBytesPerOp())
	t.Logf("─────────────────────────────")
	t.Logf("TOTAL:           %5d bytes", fullResult.AllocedBytesPerOp())
}

// TestLStateDetailedBreakdown measures each LState component
func TestLStateDetailedBreakdown(t *testing.T) {
	// Warmup
	for i := 0; i < 100; i++ {
		l := lua.NewState(lua.Options{SkipOpenLibs: true})
		l.Close()
	}

	configs := []struct {
		name string
		opts lua.Options
	}{
		{"minimal", lua.Options{
			SkipOpenLibs:        true,
			RegistrySize:        8,
			RegistryMaxSize:     64,
			RegistryGrowStep:    8,
			CallStackSize:       8,
			MinimizeStackMemory: true,
		}},
		{"small", lua.Options{
			SkipOpenLibs:        true,
			RegistrySize:        32,
			RegistryMaxSize:     256,
			RegistryGrowStep:    16,
			CallStackSize:       32,
			MinimizeStackMemory: true,
		}},
		{"default", lua.Options{
			SkipOpenLibs:        true,
			RegistrySize:        128,
			RegistryMaxSize:     256 * 256,
			RegistryGrowStep:    16,
			CallStackSize:       128,
			MinimizeStackMemory: true,
		}},
	}

	for _, cfg := range configs {
		opts := cfg.opts
		result := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				l := lua.NewState(opts)
				l.Close()
			}
		})
		t.Logf("%s: %d allocs, %d bytes (reg=%d, stack=%d)",
			cfg.name, result.AllocsPerOp(), result.AllocedBytesPerOp(),
			opts.RegistrySize, opts.CallStackSize)
	}
}
