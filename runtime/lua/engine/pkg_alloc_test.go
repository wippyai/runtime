package engine

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestPackageAllocDetail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping package allocation test in short mode")
	}
	opts := lua.Options{
		RegistrySize:        128,
		RegistryMaxSize:     256 * 256,
		RegistryGrowStep:    16,
		SkipOpenLibs:        true,
		CallStackSize:       128,
		MinimizeStackMemory: true,
	}

	// Warmup
	wl := lua.NewState(opts)
	OpenRestrictedPackage(wl)
	wl.Close()

	t.Log("=== Package allocation breakdown ===")

	// Baseline
	baseline := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := lua.NewState(opts)
			l.Close()
		}
	})
	t.Logf("Baseline (LState):        %d allocs, %d bytes", baseline.AllocsPerOp(), baseline.AllocedBytesPerOp())

	// Direct call (not via l.Call)
	directCall := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := lua.NewState(opts)
			OpenRestrictedPackage(l)
			l.Close()
		}
	})
	t.Logf("+ OpenRestrictedPackage:  %d allocs, %d bytes (delta: +%d, +%d)",
		directCall.AllocsPerOp(), directCall.AllocedBytesPerOp(),
		directCall.AllocsPerOp()-baseline.AllocsPerOp(),
		directCall.AllocedBytesPerOp()-baseline.AllocedBytesPerOp())

	// Via l.Call (current pattern)
	viaCall := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := lua.NewState(opts)
			l.Push(lua.LGoFunc(OpenRestrictedPackage))
			l.Push(lua.LString(lua.LoadLibName))
			l.Call(1, 0)
			l.Close()
		}
	})
	t.Logf("+ via l.Call pattern:     %d allocs, %d bytes (delta: +%d, +%d)",
		viaCall.AllocsPerOp(), viaCall.AllocedBytesPerOp(),
		viaCall.AllocsPerOp()-baseline.AllocsPerOp(),
		viaCall.AllocedBytesPerOp()-baseline.AllocedBytesPerOp())

	// Just creating the 3 tables manually
	justTables := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := lua.NewState(opts)
			_ = l.CreateTable(0, 6)  // packagemod
			_ = l.CreateTable(0, 16) // preload
			_ = l.CreateTable(0, 32) // loaded
			l.Close()
		}
	})
	t.Logf("Just 3 CreateTable:       %d allocs, %d bytes (delta: +%d, +%d)",
		justTables.AllocsPerOp(), justTables.AllocedBytesPerOp(),
		justTables.AllocsPerOp()-baseline.AllocsPerOp(),
		justTables.AllocedBytesPerOp()-baseline.AllocedBytesPerOp())

	t.Log("\n=== Conclusion ===")
	callOverhead := viaCall.AllocedBytesPerOp() - directCall.AllocedBytesPerOp()
	t.Logf("l.Call() overhead:        %d bytes", callOverhead)
	pkgOverhead := directCall.AllocedBytesPerOp() - justTables.AllocedBytesPerOp()
	t.Logf("Package logic overhead:   %d bytes", pkgOverhead)
}
