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
	managerCtx = &ctxapi.Key{Name: "process.manager", Scope: ctxapi.ScopeThread}

	// prototypesCtx is the context key for storing a PrototypeFactory.
	prototypesCtx = &ctxapi.Key{Name: "process.prototypes", Scope: ctxapi.ScopeThread}

	// hostsCtx is the context key for storing a HostRegistry.
	hostsCtx = &ctxapi.Key{Name: "process.hosts", Scope: ctxapi.ScopeThread}

	// onCompleteCtx is the context key for storing process completion callbacks (ScopeCall: call-specific).
	onCompleteCtx = &ctxapi.Key{Name: "process.onComplete", Scope: ctxapi.ScopeCall}

	// onStartCtx is the context key for storing process start callbacks (ScopeCall: call-specific).
	onStartCtx = &ctxapi.Key{Name: "process.onStart", Scope: ctxapi.ScopeCall}
)

// WithManager attaches a process Manager to the context.
// This makes the Manager available to any code that has access to the context.
// This is used to provide process management capabilities throughout the system
// without requiring direct dependency injection at every level.
func WithManager(ctx context.Context, m Manager) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(managerCtx) == nil {
		ac.With(managerCtx, m)
	}
	return ctx
}

// GetManager retrieves the process Manager from the context.
// Returns nil if no Manager is found in the context.
// This allows components to access the process manager without having it
// directly passed as a parameter.
func GetManager(ctx context.Context) Manager {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(managerCtx); val != nil {
		if m, ok := val.(Manager); ok {
			return m
		}
	}
	return nil
}

// WithPrototypes attaches a PrototypeFactory to the context.
func WithPrototypes(ctx context.Context, p PrototypeFactory) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(prototypesCtx) == nil {
		ac.With(prototypesCtx, p)
	}
	return ctx
}

// GetPrototypes retrieves the PrototypeFactory from the context.
func GetPrototypes(ctx context.Context) PrototypeFactory {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(prototypesCtx); val != nil {
		if p, ok := val.(PrototypeFactory); ok {
			return p
		}
	}
	return nil
}

// WithHosts attaches a HostRegistry to the context.
func WithHosts(ctx context.Context, h HostRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(hostsCtx) == nil {
		ac.With(hostsCtx, h)
	}
	return ctx
}

// GetHosts retrieves the HostRegistry from the context.
func GetHosts(ctx context.Context) HostRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(hostsCtx); val != nil {
		if h, ok := val.(HostRegistry); ok {
			return h
		}
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
