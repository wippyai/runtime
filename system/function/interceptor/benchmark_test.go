package interceptor

import (
	"context"
	"strconv"
	"testing"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"go.uber.org/zap"
)

// BenchmarkRegistryNoInterceptors benchmarks execution without interceptors
func BenchmarkRegistryNoInterceptors(b *testing.B) {
	reg := NewRegistry(zap.NewNop())

	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.NewID("bench", "func")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = reg.Execute(ctx, mockFunc, task)
	}
}

// BenchmarkRegistryOneInterceptor benchmarks registry with one interceptor
func BenchmarkRegistryOneInterceptor(b *testing.B) {
	reg := NewRegistry(zap.NewNop())
	_ = reg.Register("int1", &benchInterceptor{name: "int1"}, 100)

	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.NewID("bench", "func")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = reg.Execute(ctx, mockFunc, task)
	}
}

// BenchmarkRegistryThreeInterceptors benchmarks registry with three interceptors
func BenchmarkRegistryThreeInterceptors(b *testing.B) {
	reg := NewRegistry(zap.NewNop())
	_ = reg.Register("int1", &benchInterceptor{name: "int1"}, 100)
	_ = reg.Register("int2", &benchInterceptor{name: "int2"}, 200)
	_ = reg.Register("int3", &benchInterceptor{name: "int3"}, 300)

	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.NewID("bench", "func")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = reg.Execute(ctx, mockFunc, task)
	}
}

// BenchmarkRegistryTenInterceptors benchmarks registry with ten interceptors
func BenchmarkRegistryTenInterceptors(b *testing.B) {
	reg := NewRegistry(zap.NewNop())
	for i := 0; i < 10; i++ {
		_ = reg.Register("int"+strconv.Itoa(i), &benchInterceptor{name: "int"}, i*100)
	}

	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.NewID("bench", "func")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = reg.Execute(ctx, mockFunc, task)
	}
}

// BenchmarkContextValuePropagation benchmarks context value propagation through interceptors
func BenchmarkContextValuePropagation(b *testing.B) {
	type ctxKey string
	const testKey ctxKey = "test"

	reg := NewRegistry(zap.NewNop())
	_ = reg.Register("modifier", &benchModifyingInterceptor{key: testKey, value: "modified"}, 100)

	mockFunc := func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
		_ = ctx.Value(testKey)
		return &runtime.Result{}, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.NewID("bench", "func")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = reg.Execute(ctx, mockFunc, task)
	}
}

// BenchmarkParallelExecution benchmarks parallel registry execution
func BenchmarkParallelExecution(b *testing.B) {
	reg := NewRegistry(zap.NewNop())
	_ = reg.Register("int1", &benchInterceptor{name: "int1"}, 100)
	_ = reg.Register("int2", &benchInterceptor{name: "int2"}, 200)

	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.NewID("bench", "func")}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = reg.Execute(ctx, mockFunc, task)
		}
	})
}

// benchInterceptor is a simple interceptor for benchmarking
type benchInterceptor struct {
	name string
}

func (m *benchInterceptor) Handle(ctx context.Context, task runtime.Task, next func(context.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error) {
	return next(ctx, task)
}

// benchModifyingInterceptor modifies context for benchmarking
type benchModifyingInterceptor struct {
	key   interface{}
	value interface{}
}

func (m *benchModifyingInterceptor) Handle(ctx context.Context, task runtime.Task, next func(context.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error) {
	newCtx := context.WithValue(ctx, m.key, m.value)
	return next(newCtx, task)
}
