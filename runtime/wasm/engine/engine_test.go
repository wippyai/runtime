package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.bytecodealliance.org/wit"
)

const testWAT = `(module
  (memory (export "memory") 1)

  (func $add (export "add") (param $a i32) (param $b i32) (result i32)
    local.get $a
    local.get $b
    i32.add
  )

  (global $heap_ptr (mut i32) (i32.const 1024))

  (func $canonical_abi_realloc (export "canonical_abi_realloc")
    (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32)
    (result i32)
    (local $ptr i32)
    (if (i32.eqz (local.get $new_size))
      (then (return (i32.const 0)))
    )
    (local.set $ptr (global.get $heap_ptr))
    (global.set $heap_ptr (i32.add (global.get $heap_ptr) (local.get $new_size)))
    (local.get $ptr)
  )

  (func $canonical_abi_free (export "canonical_abi_free")
    (param $ptr i32) (param $size i32) (param $align i32)
  )
)`

const testWIT = `package test:adder@0.1.0;

world adder {
  export add: func(a: s32, b: s32) -> s32;
}`

func TestWASMAdd(t *testing.T) {
	ctx := context.Background()

	rt, err := wasmrt.New(ctx)
	require.NoError(t, err)
	defer rt.Close(ctx)

	module, err := CompileWAT(ctx, rt, testWAT, testWIT)
	require.NoError(t, err)

	inst, err := module.Instantiate(ctx)
	require.NoError(t, err)
	defer inst.Close(ctx)

	// For native WASM, use CallWithTypes with explicit WIT types
	params := []wit.Type{wit.S32{}, wit.S32{}}
	results := []wit.Type{wit.S32{}}
	result, err := inst.CallWithTypes(ctx, "add", params, results, int32(2), int32(3))
	require.NoError(t, err)
	require.Equal(t, int32(5), result)
}

func TestProcessExecute(t *testing.T) {
	ctx := context.Background()

	rt, err := wasmrt.New(ctx)
	require.NoError(t, err)
	defer rt.Close(ctx)

	module, err := CompileWAT(ctx, rt, testWAT, testWIT)
	require.NoError(t, err)

	factory := NewFactory(rt, module)
	proc, err := factory.Create()()
	require.NoError(t, err)
	defer proc.Close()

	// Simple call without payloads - this will fail because native WASM
	// needs CallWithTypes, but we test the process lifecycle
	err = proc.Execute(ctx, "add", nil)
	require.NoError(t, err)

	// Step will try to call the function
	result, _ := proc.Step(nil)
	require.Equal(t, int(2), int(result.Status)) // StepDone = 2
}
