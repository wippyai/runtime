package process

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
)

// Context keys for storing process-related information in context.Context
var (
	// managerCtx is the context key for storing a process Manager.
	managerCtx = ctxapi.Key{Name: "process.manager"}

	// onCompleteCtx is the context key for storing process completion callbacks.
	onCompleteCtx = ctxapi.Key{Name: "process.onComplete"}

	// onStartCtx is the context key for storing process start callbacks.
	onStartCtx = ctxapi.Key{Name: "process.onStart"}
)

// WithProcesses attaches a process Manager to the context.
// This makes the Manager available to any code that has access to the context.
// This is used to provide process management capabilities throughout the system
// without requiring direct dependency injection at every level.
func WithProcesses(ctx context.Context, m Manager) context.Context {
	return context.WithValue(ctx, managerCtx, m)
}

// GetProcesses retrieves the process Manager from the context.
// Returns nil if no Manager is found in the context.
// This allows components to access the process manager without having it
// directly passed as a parameter.
func GetProcesses(ctx context.Context) Manager {
	if m, ok := ctx.Value(managerCtx).(Manager); ok {
		return m
	}

	return nil
}

// OnComplete is the type for a process completion callback.
// It is called when a process finishes execution, either successfully or with an error.
// The callback receives the process ID and the execution result.
type OnComplete func(pid pubsub.PID, result *runtime.Result)

// OnStart is the type for a process start callback.
// It is called when a process begins execution.
// The callback receives the process ID and the process instance.
type OnStart func(pid pubsub.PID, proc Process)

// WithAddedOnComplete attaches an OnComplete callback to the context.
// If there's already one present, it combines them so that both are called.
// This enables composable process lifecycle management where multiple
// components can register callbacks for the same lifecycle events.
// The most recently added callback will be called first.
func WithAddedOnComplete(ctx context.Context, cb OnComplete) context.Context {
	if existing, ok := ctx.Value(onCompleteCtx).(OnComplete); ok {
		combined := func(pid pubsub.PID, result *runtime.Result) {
			cb(pid, result)
			existing(pid, result)
		}
		return context.WithValue(ctx, onCompleteCtx, OnComplete(combined))
	}

	return context.WithValue(ctx, onCompleteCtx, cb)
}

// WithAddedOnStart attaches an OnStart callback to the context.
// If there's already one present, it combines them so that both are called.
// This enables composable process lifecycle management where multiple
// components can register callbacks for the same lifecycle events.
// The most recently added callback will be called first.
func WithAddedOnStart(ctx context.Context, cb OnStart) context.Context {
	if existing, ok := ctx.Value(onStartCtx).(OnStart); ok {
		combined := func(pid pubsub.PID, proc Process) {
			cb(pid, proc)
			existing(pid, proc)
		}
		return context.WithValue(ctx, onStartCtx, OnStart(combined))
	}

	return context.WithValue(ctx, onStartCtx, cb)
}

// GetOnComplete retrieves the OnComplete callback from the context.
// Returns nil if no callback is found.
// This is typically used by process supervisors to get the callback
// that should be invoked when a process completes.
func GetOnComplete(ctx context.Context) OnComplete {
	if cb, ok := ctx.Value(onCompleteCtx).(OnComplete); ok {
		return cb
	}
	return nil
}

// GetOnStart retrieves the OnStart callback from the context.
// Returns nil if no callback is found.
// This is typically used by process hosts to get the callback
// that should be invoked when a process starts.
func GetOnStart(ctx context.Context) OnStart {
	if cb, ok := ctx.Value(onStartCtx).(OnStart); ok {
		return cb
	}
	return nil
}
