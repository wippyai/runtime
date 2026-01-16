package wasm

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
)

type contextKey int

const (
	asyncFrameKey contextKey = iota
)

// Asyncify represents the asyncify state machine interface.
// Implemented by the wasm-runtime engine package.
type Asyncify interface {
	IsNormal(ctx context.Context) bool
	IsUnwinding(ctx context.Context) bool
	IsRewinding(ctx context.Context) bool
	StartUnwind(ctx context.Context) error
	StopUnwind(ctx context.Context) error
	StartRewind(ctx context.Context) error
	StopRewind(ctx context.Context) error
	ResetStack()
}

// AsyncScheduler stores pending commands and results for async operations.
// Implemented by the Process.
type AsyncScheduler interface {
	SetPending(cmd dispatcher.Command)
	GetResult() (uint64, error)
	ClearPending()
}

// AsyncFrame contains asyncify and scheduler for host functions.
type AsyncFrame struct {
	Asyncify  Asyncify
	Scheduler AsyncScheduler
}

// WithAsyncFrame adds the async frame to context.
func WithAsyncFrame(ctx context.Context, frame *AsyncFrame) context.Context {
	return context.WithValue(ctx, asyncFrameKey, frame)
}

// GetAsyncFrame retrieves the async frame from context.
func GetAsyncFrame(ctx context.Context) *AsyncFrame {
	if v := ctx.Value(asyncFrameKey); v != nil {
		return v.(*AsyncFrame)
	}
	return nil
}
