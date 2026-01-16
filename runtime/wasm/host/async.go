package host

import (
	"context"

	"github.com/tetratelabs/wazero/api"

	"github.com/wippyai/runtime/api/dispatcher"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
)

// MakeAsyncHandler creates a wazero host function that supports asyncify.
// The createCmd function creates a dispatcher command from the stack args.
// T must implement dispatcher.Command.
func MakeAsyncHandler[T dispatcher.Command](createCmd func(stack []uint64) T) api.GoModuleFunc {
	return func(ctx context.Context, mod api.Module, stack []uint64) {
		frame := wasmapi.GetAsyncFrame(ctx)
		if frame == nil || frame.Asyncify == nil || frame.Scheduler == nil {
			return
		}

		// Check if rewinding (resuming from suspend)
		if frame.Asyncify.IsRewinding(ctx) {
			result, _ := frame.Scheduler.GetResult()
			if len(stack) > 0 {
				stack[0] = result
			}
			_ = frame.Asyncify.StopRewind(ctx)
			frame.Scheduler.ClearPending()
			return
		}

		// Create command and suspend
		cmd := createCmd(stack)
		frame.Scheduler.SetPending(cmd)
		// StartUnwind error is ignored - scheduler will handle invalid state
		_ = frame.Asyncify.StartUnwind(ctx)
	}
}

// wrapHostFunc wraps a host function to the api.GoModuleFunc signature.
func wrapHostFunc(fn any) api.GoModuleFunc {
	switch f := fn.(type) {
	case api.GoModuleFunc:
		return f
	case func(ctx context.Context, mod api.Module, stack []uint64):
		return f
	case func(ctx context.Context) int64:
		return func(ctx context.Context, mod api.Module, stack []uint64) {
			result := f(ctx)
			if len(stack) > 0 {
				stack[0] = uint64(result)
			}
		}
	case func(ctx context.Context, arg int64):
		return func(ctx context.Context, mod api.Module, stack []uint64) {
			var arg int64
			if len(stack) > 0 {
				arg = int64(stack[0])
			}
			f(ctx, arg)
		}
	case func(ctx context.Context, arg int64) int64:
		return func(ctx context.Context, mod api.Module, stack []uint64) {
			var arg int64
			if len(stack) > 0 {
				arg = int64(stack[0])
			}
			result := f(ctx, arg)
			if len(stack) > 0 {
				stack[0] = uint64(result)
			}
		}
	default:
		return func(ctx context.Context, mod api.Module, stack []uint64) {}
	}
}
