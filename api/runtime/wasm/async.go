package wasm

import (
	"context"
	"io"

	appctx "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
)

// asyncFrameKey is the FrameContext key for WASM async state.
var asyncFrameKey = &appctx.Key{Name: "wasm.async"}

// ResourceCloser is implemented by resource managers that need cleanup.
type ResourceCloser interface {
	io.Closer
}

// AsyncFrame holds scheduler, asyncify, and resources for single frame lookup.
type AsyncFrame struct {
	Scheduler dispatcher.AsyncScheduler
	Asyncify  *wasmengine.Asyncify
	Resources ResourceCloser
}

// SetAsyncFrame stores async frame in FrameContext.
func SetAsyncFrame(ctx context.Context, f *AsyncFrame) error {
	fc := appctx.FrameFromContext(ctx)
	if fc == nil {
		return appctx.ErrNoFrameContext
	}
	return fc.Set(asyncFrameKey, f)
}

// GetAsyncFrame retrieves async frame from FrameContext (single lookup).
func GetAsyncFrame(ctx context.Context) *AsyncFrame {
	fc := appctx.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if v, ok := fc.Get(asyncFrameKey); ok {
		return v.(*AsyncFrame)
	}
	return nil
}

// WithScheduler adds scheduler to frame context.
func WithScheduler(ctx context.Context, s dispatcher.AsyncScheduler) context.Context {
	if f := GetAsyncFrame(ctx); f != nil {
		f.Scheduler = s
		return ctx
	}
	// Create new frame if needed
	fc := appctx.FrameFromContext(ctx)
	if fc == nil {
		ctx, fc = appctx.AcquireFrameContext(ctx)
	}
	_ = fc.Set(asyncFrameKey, &AsyncFrame{Scheduler: s})
	return ctx
}

// GetScheduler retrieves scheduler from frame context.
func GetScheduler(ctx context.Context) dispatcher.AsyncScheduler {
	if f := GetAsyncFrame(ctx); f != nil {
		return f.Scheduler
	}
	return nil
}

// WithAsyncify adds asyncify to frame context.
func WithAsyncify(ctx context.Context, a *wasmengine.Asyncify) context.Context {
	if f := GetAsyncFrame(ctx); f != nil {
		f.Asyncify = a
		return ctx
	}
	// Create new frame if needed
	fc := appctx.FrameFromContext(ctx)
	if fc == nil {
		ctx, fc = appctx.AcquireFrameContext(ctx)
	}
	_ = fc.Set(asyncFrameKey, &AsyncFrame{Asyncify: a})
	return ctx
}

// GetAsyncify retrieves asyncify from frame context.
func GetAsyncify(ctx context.Context) *wasmengine.Asyncify {
	if f := GetAsyncFrame(ctx); f != nil {
		return f.Asyncify
	}
	return nil
}

// WithResources adds resources to frame context.
func WithResources(ctx context.Context, r ResourceCloser) context.Context {
	if f := GetAsyncFrame(ctx); f != nil {
		f.Resources = r
		return ctx
	}
	// Create new frame if needed
	fc := appctx.FrameFromContext(ctx)
	if fc == nil {
		ctx, fc = appctx.AcquireFrameContext(ctx)
	}
	_ = fc.Set(asyncFrameKey, &AsyncFrame{Resources: r})
	return ctx
}

// GetResources retrieves resources from frame context.
func GetResources(ctx context.Context) ResourceCloser {
	if f := GetAsyncFrame(ctx); f != nil {
		return f.Resources
	}
	return nil
}

// CloseResources closes and clears resources from the async frame.
func CloseResources(ctx context.Context) error {
	if f := GetAsyncFrame(ctx); f != nil && f.Resources != nil {
		err := f.Resources.Close()
		f.Resources = nil
		return err
	}
	return nil
}
