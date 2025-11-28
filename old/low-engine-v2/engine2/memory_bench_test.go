package engine2

import (
	"context"
	"runtime"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	lua "github.com/yuin/gopher-lua"
)

func measureMemory() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc
}

func BenchmarkMemoryBaseline(b *testing.B) {
	script := `return 1`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithProto(proto))
		if err := proc.Start(ctx, nil); err != nil {
			b.Fatal(err)
		}
		proc.Close()
	}
}

func BenchmarkMemoryWithLibs(b *testing.B) {
	script := `return 1`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(
			WithProto(proto),
			WithModuleBinder(BindTimeSleep),
		)
		if err := proc.Start(ctx, nil); err != nil {
			b.Fatal(err)
		}
		proc.Close()
	}
}

func TestMemoryPerProcessDetailed(t *testing.T) {
	script := `return 1`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	const count = 100
	processes := make([]*Process, count)

	before := measureMemory()

	for i := 0; i < count; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithProto(proto))
		if err := proc.Start(ctx, nil); err != nil {
			t.Fatal(err)
		}
		processes[i] = proc
	}

	after := measureMemory()

	perProcess := (after - before) / count
	t.Logf("Memory per process (baseline): %d bytes (~%.1f KB)", perProcess, float64(perProcess)/1024)

	for _, p := range processes {
		p.Close()
	}
}

func TestMemoryWithTimeSleep(t *testing.T) {
	script := `return 1`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	const count = 100
	processes := make([]*Process, count)

	before := measureMemory()

	for i := 0; i < count; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(
			WithProto(proto),
			WithModuleBinder(BindTimeSleep),
		)
		if err := proc.Start(ctx, nil); err != nil {
			t.Fatal(err)
		}
		processes[i] = proc
	}

	after := measureMemory()

	perProcess := (after - before) / count
	t.Logf("Memory per process (with time module): %d bytes (~%.1f KB)", perProcess, float64(perProcess)/1024)

	for _, p := range processes {
		p.Close()
	}
}
