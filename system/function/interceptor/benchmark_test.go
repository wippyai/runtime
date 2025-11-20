package interceptor

import (
	"context"
	"testing"

	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"go.uber.org/zap"
)

// BenchmarkChainExecuteNoInterceptors benchmarks chain execution without interceptors
func BenchmarkChainExecuteNoInterceptors(b *testing.B) {
	chain := newChain(nil, zap.NewNop())

	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "bench", Name: "func"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chain.Execute(ctx, mockFunc, task)
	}
}

// BenchmarkChainExecuteOneInterceptor benchmarks chain with one interceptor
func BenchmarkChainExecuteOneInterceptor(b *testing.B) {
	int1 := &benchInterceptor{name: "int1"}
	chain := newChain([]function.Interceptor{int1}, zap.NewNop())

	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "bench", Name: "func"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chain.Execute(ctx, mockFunc, task)
	}
}

// BenchmarkChainExecuteThreeInterceptors benchmarks chain with three interceptors
func BenchmarkChainExecuteThreeInterceptors(b *testing.B) {
	int1 := &benchInterceptor{name: "int1"}
	int2 := &benchInterceptor{name: "int2"}
	int3 := &benchInterceptor{name: "int3"}
	chain := newChain([]function.Interceptor{int1, int2, int3}, zap.NewNop())

	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "bench", Name: "func"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chain.Execute(ctx, mockFunc, task)
	}
}

// BenchmarkChainExecuteTenInterceptors benchmarks chain with ten interceptors
func BenchmarkChainExecuteTenInterceptors(b *testing.B) {
	interceptors := make([]function.Interceptor, 10)
	for i := 0; i < 10; i++ {
		interceptors[i] = &benchInterceptor{name: "int"}
	}
	chain := newChain(interceptors, zap.NewNop())

	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "bench", Name: "func"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chain.Execute(ctx, mockFunc, task)
	}
}

// BenchmarkRegistryGetChain benchmarks getting chain from registry
func BenchmarkRegistryGetChain(b *testing.B) {
	reg := NewInterceptorRegistry(zap.NewNop())

	int1 := &benchInterceptor{name: "int1"}
	int2 := &benchInterceptor{name: "int2"}
	int3 := &benchInterceptor{name: "int3"}

	_ = reg.Register("int1", int1, 100)
	_ = reg.Register("int2", int2, 200)
	_ = reg.Register("int3", int3, 300)

	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "bench", Name: "func"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = reg.Execute(ctx, mockFunc, task)
	}
}

// BenchmarkContextValuePropagation benchmarks context value propagation through chain
func BenchmarkContextValuePropagation(b *testing.B) {
	type ctxKey string
	const testKey ctxKey = "test"

	modifyingInterceptor := &benchModifyingInterceptor{key: testKey, value: "modified"}

	chain := newChain([]function.Interceptor{modifyingInterceptor}, zap.NewNop())

	mockFunc := func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
		_ = ctx.Value(testKey)
		return &runtime.Result{}, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "bench", Name: "func"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chain.Execute(ctx, mockFunc, task)
	}
}

// BenchmarkParallelChainExecution benchmarks parallel chain execution
func BenchmarkParallelChainExecution(b *testing.B) {
	int1 := &benchInterceptor{name: "int1"}
	int2 := &benchInterceptor{name: "int2"}
	chain := newChain([]function.Interceptor{int1, int2}, zap.NewNop())

	mockFunc := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "bench", Name: "func"}}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = chain.Execute(ctx, mockFunc, task)
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
