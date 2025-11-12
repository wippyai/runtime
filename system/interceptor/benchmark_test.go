package interceptor

import (
	"context"
	"testing"

	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
)

// BenchmarkChainExecuteNoInterceptors benchmarks chain execution without interceptors
func BenchmarkChainExecuteNoInterceptors(b *testing.B) {
	chain := newChain(nil)

	mockFunc := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		ch := make(chan *runtime.Result, 1)
		ch <- &runtime.Result{}
		return ch, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "bench", Name: "func"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, _ := chain.Execute(ctx, mockFunc, task)
		<-ch
	}
}

// BenchmarkChainExecuteOneInterceptor benchmarks chain with one interceptor
func BenchmarkChainExecuteOneInterceptor(b *testing.B) {
	int1 := &mockInterceptor{name: "int1"}
	chain := newChain([]apiinterceptor.Interceptor{int1})

	mockFunc := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		ch := make(chan *runtime.Result, 1)
		ch <- &runtime.Result{}
		return ch, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "bench", Name: "func"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, _ := chain.Execute(ctx, mockFunc, task)
		<-ch
	}
}

// BenchmarkChainExecuteThreeInterceptors benchmarks chain with three interceptors
func BenchmarkChainExecuteThreeInterceptors(b *testing.B) {
	int1 := &mockInterceptor{name: "int1"}
	int2 := &mockInterceptor{name: "int2"}
	int3 := &mockInterceptor{name: "int3"}
	chain := newChain([]apiinterceptor.Interceptor{int1, int2, int3})

	mockFunc := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		ch := make(chan *runtime.Result, 1)
		ch <- &runtime.Result{}
		return ch, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "bench", Name: "func"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, _ := chain.Execute(ctx, mockFunc, task)
		<-ch
	}
}

// BenchmarkChainExecuteTenInterceptors benchmarks chain with ten interceptors
func BenchmarkChainExecuteTenInterceptors(b *testing.B) {
	interceptors := make([]apiinterceptor.Interceptor, 10)
	for i := 0; i < 10; i++ {
		interceptors[i] = &mockInterceptor{name: "int"}
	}
	chain := newChain(interceptors)

	mockFunc := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		ch := make(chan *runtime.Result, 1)
		ch <- &runtime.Result{}
		return ch, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "bench", Name: "func"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, _ := chain.Execute(ctx, mockFunc, task)
		<-ch
	}
}

// BenchmarkRegistryGetChain benchmarks getting chain from registry
func BenchmarkRegistryGetChain(b *testing.B) {
	reg, _ := setupRegistryTest()
	reg.Start(context.Background())
	defer reg.Stop()

	int1 := &mockInterceptor{name: "int1"}
	int2 := &mockInterceptor{name: "int2"}
	int3 := &mockInterceptor{name: "int3"}
	reg.interceptors = []apiinterceptor.Interceptor{int1, int2, int3}

	mockFunc := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		ch := make(chan *runtime.Result, 1)
		ch <- &runtime.Result{}
		return ch, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "bench", Name: "func"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, _ := reg.Execute(ctx, mockFunc, task)
		<-ch
	}
}

// BenchmarkRegistryRegister benchmarks interceptor registration
func BenchmarkRegistryRegister(b *testing.B) {
	reg, _ := setupRegistryTest()
	reg.Start(context.Background())
	defer reg.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		reg.interceptors = nil // Reset
		interceptor := &mockInterceptor{name: "test"}
		b.StartTimer()

		reg.Register("test", interceptor)
	}
}

// BenchmarkContextValuePropagation benchmarks context value propagation through chain
func BenchmarkContextValuePropagation(b *testing.B) {
	type ctxKey string
	const testKey ctxKey = "test"

	modifyingInterceptor := apiinterceptor.InterceptorFunc(func(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context) {
		newCtx := context.WithValue(ctx, testKey, "modified")
		return next(newCtx)
	})

	chain := newChain([]apiinterceptor.Interceptor{modifyingInterceptor})

	mockFunc := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		_ = ctx.Value(testKey)
		ch := make(chan *runtime.Result, 1)
		ch <- &runtime.Result{}
		return ch, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "bench", Name: "func"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch, _ := chain.Execute(ctx, mockFunc, task)
		<-ch
	}
}

// BenchmarkParallelChainExecution benchmarks parallel chain execution
func BenchmarkParallelChainExecution(b *testing.B) {
	int1 := &mockInterceptor{name: "int1"}
	int2 := &mockInterceptor{name: "int2"}
	chain := newChain([]apiinterceptor.Interceptor{int1, int2})

	mockFunc := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		ch := make(chan *runtime.Result, 1)
		ch <- &runtime.Result{}
		return ch, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "bench", Name: "func"}}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ch, _ := chain.Execute(ctx, mockFunc, task)
			<-ch
		}
	})
}
