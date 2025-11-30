package random

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero/api"
)

func TestRandomHost(t *testing.T) {
	host := New()

	t.Run("info returns correct namespace", func(t *testing.T) {
		info := host.Info()
		if info.Namespace != Namespace {
			t.Errorf("namespace = %s, want %s", info.Namespace, Namespace)
		}
	})

	t.Run("register returns functions", func(t *testing.T) {
		reg := host.Register()

		if reg.Functions == nil {
			t.Error("expected functions")
		}

		expectedFuncs := []string{
			"get-random-bytes",
			"get-random-u64",
		}

		for _, name := range expectedFuncs {
			if _, ok := reg.Functions[name]; !ok {
				t.Errorf("missing function: %s", name)
			}
		}
	})

	t.Run("no yield types for sync functions", func(t *testing.T) {
		reg := host.Register()
		if len(reg.YieldTypes) != 0 {
			t.Errorf("yield types = %d, want 0 for sync random", len(reg.YieldTypes))
		}
	})
}

func BenchmarkGetRandomU64(b *testing.B) {
	host := New()
	fn := host.Register().Functions["get-random-u64"].(func(context.Context, api.Module, []uint64))
	stack := make([]uint64, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fn(context.Background(), nil, stack)
	}
}
