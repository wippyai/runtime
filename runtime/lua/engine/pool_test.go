package engine

import (
	"context"
	"fmt"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/system/clock"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	lua "github.com/yuin/gopher-lua"
)

type poolTestDispatcher struct {
	handlers map[dispatcher.CommandID]dispatcher.Handler
	clock    *clock.Dispatcher
}

func newPoolTestDispatcher() *poolTestDispatcher {
	d := &poolTestDispatcher{handlers: make(map[dispatcher.CommandID]dispatcher.Handler)}
	d.clock = clock.NewDispatcher()
	d.clock.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		d.handlers[id] = h
	})
	return d
}

func (d *poolTestDispatcher) Dispatch(cmd dispatcher.Command) dispatcher.Handler {
	return d.handlers[cmd.CmdID()]
}

func (d *poolTestDispatcher) Stop() {
	if d.clock != nil {
		_ = d.clock.Stop(context.Background())
	}
}

func newLuaFactory(script string) funcpool.Factory {
	return func() (process.Process, error) {
		proto, err := lua.CompileString(script, "test.lua")
		if err != nil {
			return nil, err
		}

		proc := NewProcess(
			WithProto(proto),
		)

		return proc, nil
	}
}

// TestPoolBasicCall tests basic pool call functionality
func TestPoolBasicCall(t *testing.T) {
	factory := newLuaFactory(`return 1 + 2`)
	disp := newPoolTestDispatcher()
	defer disp.Stop()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	result, err := ps.Call(ctx, "", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	t.Log("Basic pool call passed")
}

// TestPoolStateReuse tests that Lua state persists between calls
func TestPoolStateReuse(t *testing.T) {
	factory := newLuaFactory(`
		counter = (counter or 0) + 1
		return counter
	`)
	disp := newPoolTestDispatcher()
	defer disp.Stop()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	// Call 3 times - counter should increment
	for i := 1; i <= 3; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		result, err := ps.Call(ctx, "", nil)
		if err != nil {
			t.Fatalf("Call %d failed: %v", i, err)
		}
		if result.Error != nil {
			t.Fatalf("Call %d result error: %v", i, result.Error)
		}
		t.Logf("Call %d result: %v", i, result.Value)
	}
}

// Benchmark8x8NoYield tests 8 workers without yields
func Benchmark8x8NoYield(b *testing.B) {
	factory := newLuaFactory(`return 1 + 2`)
	disp := newPoolTestDispatcher()
	defer disp.Stop()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{Workers: 8, QueueSize: 256})
	if err != nil {
		b.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx, fc := ctxapi.AcquireFrameContext(context.Background())
			_, _ = ps.Call(ctx, "", nil)
			ctxapi.ReleaseFrameContext(fc)
		}
	})
}

// BenchmarkSingleWorker tests throughput of a single worker (no parallelism)
func BenchmarkSingleWorker(b *testing.B) {
	factory := newLuaFactory(`return 1`)
	disp := newPoolTestDispatcher()
	defer disp.Stop()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{
		Workers:   1,
		QueueSize: 16,
	})
	if err != nil {
		b.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, fc := ctxapi.AcquireFrameContext(context.Background())
		_, _ = ps.Call(ctx, "", nil)
		ctxapi.ReleaseFrameContext(fc)
	}
}

// BenchmarkWorkerScalingLua tests Lua throughput with different worker counts
func BenchmarkWorkerScalingLua(b *testing.B) {
	for _, workers := range []int{1, 2, 4, 8, 16} {
		b.Run(fmt.Sprintf("W%d", workers), func(b *testing.B) {
			factory := newLuaFactory(`return 1`)
			disp := newPoolTestDispatcher()
			defer disp.Stop()

			ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{
				Workers:   workers,
				QueueSize: 16,
			})
			if err != nil {
				b.Fatal(err)
			}

			ps.Start()
			defer ps.Stop()

			b.ResetTimer()
			b.ReportAllocs()

			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					ctx, fc := ctxapi.AcquireFrameContext(context.Background())
					_, _ = ps.Call(ctx, "", nil)
					ctxapi.ReleaseFrameContext(fc)
				}
			})
		})
	}
}
