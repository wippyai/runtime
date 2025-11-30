package clocks

import (
	"context"
	"testing"
	"time"

	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/runtime/runtime/wasm/resource"
)

func TestWallClockHost(t *testing.T) {
	host := NewWallClockHost()

	t.Run("info returns correct namespace", func(t *testing.T) {
		info := host.Info()
		if info.Namespace != WallClockNamespace {
			t.Errorf("namespace = %s, want %s", info.Namespace, WallClockNamespace)
		}
	})

	t.Run("register returns functions", func(t *testing.T) {
		reg := host.Register()

		if reg.Functions == nil {
			t.Error("expected functions")
		}

		expectedFuncs := []string{"now", "resolution"}
		for _, name := range expectedFuncs {
			if _, ok := reg.Functions[name]; !ok {
				t.Errorf("missing function: %s", name)
			}
		}
	})

	t.Run("no yield types for sync functions", func(t *testing.T) {
		reg := host.Register()
		if len(reg.YieldTypes) != 0 {
			t.Errorf("yield types = %d, want 0", len(reg.YieldTypes))
		}
	})
}

func TestMonotonicClockHost(t *testing.T) {
	resources := resource.NewInstanceResources()
	defer resources.Close()

	host := NewMonotonicClockHost(resources)

	t.Run("info returns correct namespace", func(t *testing.T) {
		info := host.Info()
		if info.Namespace != MonotonicClockNamespace {
			t.Errorf("namespace = %s, want %s", info.Namespace, MonotonicClockNamespace)
		}
	})

	t.Run("resources returns shared resources", func(t *testing.T) {
		if host.Resources() != resources {
			t.Error("expected same resources instance")
		}
	})

	t.Run("register returns functions", func(t *testing.T) {
		reg := host.Register()

		if reg.Functions == nil {
			t.Error("expected functions")
		}

		expectedFuncs := []string{
			"now",
			"resolution",
			"subscribe-instant",
			"subscribe-duration",
		}
		for _, name := range expectedFuncs {
			if _, ok := reg.Functions[name]; !ok {
				t.Errorf("missing function: %s", name)
			}
		}
	})

	t.Run("subscribe-duration creates pollable", func(t *testing.T) {
		stack := []uint64{uint64(50 * time.Millisecond)}

		fn := host.Register().Functions["subscribe-duration"].(func(context.Context, api.Module, []uint64))
		fn(context.Background(), nil, stack)

		handle := resource.Handle(stack[0])
		if handle == 0 {
			t.Error("expected non-zero handle")
		}

		// Check pollable was created
		p, ok := resources.Pollables().Get(handle)
		if !ok {
			t.Error("expected pollable in table")
		}
		if p.Ready {
			t.Error("expected pollable to not be ready")
		}

		// Check duration was stored
		duration, ok := resources.TimerDurations().Load(handle)
		if !ok {
			t.Error("expected duration in timer map")
		}
		if duration != 50*time.Millisecond {
			t.Errorf("duration = %v, want 50ms", duration)
		}
	})

	t.Run("subscribe-duration with zero duration creates ready pollable", func(t *testing.T) {
		stack := []uint64{0}

		fn := host.Register().Functions["subscribe-duration"].(func(context.Context, api.Module, []uint64))
		fn(context.Background(), nil, stack)

		handle := resource.Handle(stack[0])
		p, ok := resources.Pollables().Get(handle)
		if !ok {
			t.Error("expected pollable")
		}
		if !p.Ready {
			t.Error("expected pollable to be ready for zero duration")
		}
	})
}

func TestMonotonicStart(t *testing.T) {
	start := MonotonicStart()
	if start.IsZero() {
		t.Error("monotonic start should not be zero")
	}
	if time.Since(start) < 0 {
		t.Error("monotonic start should be in the past")
	}
}

func BenchmarkMonotonicNow(b *testing.B) {
	resources := resource.NewInstanceResources()
	defer resources.Close()

	host := NewMonotonicClockHost(resources)
	fn := host.Register().Functions["now"].(func(context.Context, api.Module, []uint64))
	stack := make([]uint64, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fn(context.Background(), nil, stack)
	}
}

func BenchmarkSubscribeDuration(b *testing.B) {
	resources := resource.NewInstanceResources()
	defer resources.Close()

	host := NewMonotonicClockHost(resources)
	fn := host.Register().Functions["subscribe-duration"].(func(context.Context, api.Module, []uint64))
	stack := make([]uint64, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stack[0] = uint64(50 * time.Millisecond)
		fn(context.Background(), nil, stack)
		// Remove to return pollable to pool
		resources.Table().Remove(resource.Handle(stack[0]))
	}
}

func BenchmarkWallClockNow(b *testing.B) {
	host := NewWallClockHost()
	fn := host.Register().Functions["now"].(func(context.Context, api.Module, []uint64))
	stack := make([]uint64, 2)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fn(context.Background(), nil, stack)
	}
}
