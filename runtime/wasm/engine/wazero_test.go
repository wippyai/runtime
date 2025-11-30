package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero/api"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.bytecodealliance.org/wit"
)

// wasmFuncHandler wraps a WASM module for use as function handler.
// This simulates what Manager.Execute does.
type wasmFuncHandler struct {
	runtime *wasmrt.Runtime
	module  *wasmrt.Module
	method  string
}

func (h *wasmFuncHandler) Call(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	inst, err := h.module.Instantiate(ctx)
	if err != nil {
		return &runtime.Result{Error: err}, nil
	}
	defer inst.Close(ctx)

	// Extract args from payloads
	var args []any
	for _, pl := range task.Payloads {
		if data := pl.Data(); data != nil {
			args = append(args, data)
		}
	}

	// For native WASM, use CallWithTypes with explicit WIT types
	params := make([]wit.Type, len(args))
	for i := range args {
		params[i] = wit.S32{}
	}
	results := []wit.Type{wit.S32{}}

	// Convert args to int32
	int32Args := make([]any, len(args))
	for i, arg := range args {
		switch v := arg.(type) {
		case int:
			int32Args[i] = int32(v)
		case int32:
			int32Args[i] = v
		case int64:
			int32Args[i] = int32(v)
		case float64:
			int32Args[i] = int32(v)
		default:
			return &runtime.Result{Error: fmt.Errorf("unsupported arg type: %T", arg)}, nil
		}
	}

	result, err := inst.CallWithTypes(ctx, h.method, params, results, int32Args...)
	if err != nil {
		return &runtime.Result{Error: err}, nil
	}

	return &runtime.Result{Value: payload.New(result)}, nil
}

// TestWASMAsRegisteredFunction tests calling a WASM function via the function registry pattern.
// This simulates how Lua would call WASM through the funcs.call → dispatcher → function handler chain.
func TestWASMAsRegisteredFunction(t *testing.T) {
	ctx := context.Background()

	// Setup WASM runtime and module
	rt, err := wasmrt.New(ctx)
	require.NoError(t, err)
	defer rt.Close(ctx)

	module, err := CompileWAT(ctx, rt, testWAT, testWIT)
	require.NoError(t, err)

	// Create handler that wraps WASM module
	handler := &wasmFuncHandler{
		runtime: rt,
		module:  module,
		method:  "add",
	}

	// Simulate what happens when Lua calls funcs.call("wasm:add", 2, 3)
	task := runtime.Task{
		ID: registry.ParseID("wasm:add"),
		Payloads: payload.Payloads{
			payload.New(2),
			payload.New(3),
		},
	}

	// Execute via handler (what dispatcher would do)
	result, err := handler.Call(ctx, task)
	require.NoError(t, err)
	require.Nil(t, result.Error)
	require.Equal(t, int32(5), result.Value.Data())
}

// TestWASMMultipleCallsViaHandler tests multiple WASM calls via handler.
func TestWASMMultipleCallsViaHandler(t *testing.T) {
	ctx := context.Background()

	rt, err := wasmrt.New(ctx)
	require.NoError(t, err)
	defer rt.Close(ctx)

	module, err := CompileWAT(ctx, rt, testWAT, testWIT)
	require.NoError(t, err)

	handler := &wasmFuncHandler{
		runtime: rt,
		module:  module,
		method:  "add",
	}

	// Simulate a loop: sum = 0; for i=1 to 5: sum = add(sum, i)
	sum := int32(0)
	for i := int32(1); i <= 5; i++ {
		task := runtime.Task{
			ID: registry.ParseID("wasm:add"),
			Payloads: payload.Payloads{
				payload.New(sum),
				payload.New(i),
			},
		}
		result, err := handler.Call(ctx, task)
		require.NoError(t, err)
		require.Nil(t, result.Error)
		sum = result.Value.Data().(int32)
	}

	require.Equal(t, int32(15), sum) // 1+2+3+4+5 = 15
}

// TestDirectWASMFunction tests using WASM function manager pattern.
// Note: This test requires a component module, not native WAT.
// Native WASM uses CallWithTypes, which TestWASMAsRegisteredFunction covers.
func TestDirectWASMFunction(t *testing.T) {
	t.Skip("Native WASM requires CallWithTypes; use TestWASMAsRegisteredFunction for handler pattern")
}

// TestRegistryIDParsing verifies registry.ID parsing for function paths.
func TestRegistryIDParsing(t *testing.T) {
	id := registry.ParseID("wasm:add")
	require.Equal(t, "wasm", id.NS)
	require.Equal(t, "add", id.Name)

	id2 := registry.ParseID("app.wasm:calculator")
	require.Equal(t, "app.wasm", id2.NS)
	require.Equal(t, "calculator", id2.Name)
}

// TestWASMHandlerConcurrency tests concurrent WASM calls.
func TestWASMHandlerConcurrency(t *testing.T) {
	ctx := context.Background()

	rt, err := wasmrt.New(ctx)
	require.NoError(t, err)
	defer rt.Close(ctx)

	module, err := CompileWAT(ctx, rt, testWAT, testWIT)
	require.NoError(t, err)

	handler := &wasmFuncHandler{
		runtime: rt,
		module:  module,
		method:  "add",
	}

	// Run 100 concurrent calls
	const numCalls = 100
	results := make(chan int32, numCalls)
	errors := make(chan error, numCalls)

	for i := 0; i < numCalls; i++ {
		go func(a, b int32) {
			task := runtime.Task{
				ID: registry.ParseID("wasm:add"),
				Payloads: payload.Payloads{
					payload.New(a),
					payload.New(b),
				},
			}
			result, err := handler.Call(ctx, task)
			if err != nil {
				errors <- err
				return
			}
			if result.Error != nil {
				errors <- result.Error
				return
			}
			results <- result.Value.Data().(int32)
		}(int32(i), int32(i+1))
	}

	// Collect results
	for i := 0; i < numCalls; i++ {
		select {
		case r := <-results:
			// Each result should be i + (i+1) = 2i+1
			// We can't verify exact value since goroutine order is random,
			// but we know the range: 0+1=1 to 99+100=199
			require.GreaterOrEqual(t, r, int32(1))
			require.LessOrEqual(t, r, int32(199))
		case err := <-errors:
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

// BenchmarkWASMFunctionCall_NewInstance benchmarks with fresh instance per call (wrong pattern).
func BenchmarkWASMFunctionCall_NewInstance(b *testing.B) {
	ctx := context.Background()

	rt, err := wasmrt.New(ctx)
	require.NoError(b, err)
	defer rt.Close(ctx)

	module, err := CompileWAT(ctx, rt, testWAT, testWIT)
	require.NoError(b, err)

	handler := &wasmFuncHandler{
		runtime: rt,
		module:  module,
		method:  "add",
	}

	task := runtime.Task{
		ID: registry.ParseID("wasm:add"),
		Payloads: payload.Payloads{
			payload.New(int32(2)),
			payload.New(int32(3)),
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := handler.Call(ctx, task)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWASMFunctionCall_ReusedInstance benchmarks with reused instance (correct pattern).
func BenchmarkWASMFunctionCall_ReusedInstance(b *testing.B) {
	ctx := context.Background()

	rt, err := wasmrt.New(ctx)
	require.NoError(b, err)
	defer rt.Close(ctx)

	module, err := CompileWAT(ctx, rt, testWAT, testWIT)
	require.NoError(b, err)

	// Create instance ONCE
	inst, err := module.Instantiate(ctx)
	require.NoError(b, err)
	defer inst.Close(ctx)

	params := []wit.Type{wit.S32{}, wit.S32{}}
	results := []wit.Type{wit.S32{}}
	args := []any{int32(2), int32(3)}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := inst.CallWithTypes(ctx, "add", params, results, args...)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWASMFunctionCall_DirectAPI benchmarks raw wazero function call.
func BenchmarkWASMFunctionCall_DirectAPI(b *testing.B) {
	ctx := context.Background()

	rt, err := wasmrt.New(ctx)
	require.NoError(b, err)
	defer rt.Close(ctx)

	module, err := CompileWAT(ctx, rt, testWAT, testWIT)
	require.NoError(b, err)

	inst, err := module.Instantiate(ctx)
	require.NoError(b, err)
	defer inst.Close(ctx)

	// Get raw function
	rawFn := inst.GetExportedFunction("add")
	require.NotNil(b, rawFn)
	fn := rawFn.(api.Function)

	args := []uint64{2, 3}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := fn.Call(ctx, args...)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWASMProcess_Reused benchmarks Process with instance reuse (pool pattern).
func BenchmarkWASMProcess_Reused(b *testing.B) {
	ctx := context.Background()

	rt, err := wasmrt.New(ctx)
	require.NoError(b, err)
	defer rt.Close(ctx)

	module, err := CompileWAT(ctx, rt, testWAT, testWIT)
	require.NoError(b, err)

	// Create process with pre-initialized instance (like pool worker)
	proc := NewProcess(rt, module)
	require.NoError(b, proc.Init(ctx))
	defer proc.Close()

	input := payload.Payloads{
		payload.New(int32(2)),
		payload.New(int32(3)),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := proc.Execute(ctx, "add", input)
		if err != nil {
			b.Fatal(err)
		}
		// Step would be called by executor, but for benchmark we just test Execute overhead
	}
}
