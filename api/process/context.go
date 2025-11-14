// Package process provides process abstraction and lifecycle.
package process

import (
	"context"
	"errors"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// PrototypeFactory manages process prototypes.
type PrototypeFactory interface {
	Start(ctx context.Context) error
	Stop() error
}

// HostRegistry manages process hosts.
type HostRegistry interface {
	Start(ctx context.Context) error
	Stop() error
}

// Context keys for storing process-related information in context.Context
var (
	// managerCtx is the context key for storing a process Manager.
	managerCtx = &ctxapi.Key{Name: "process.manager"}

	// prototypesCtx is the context key for storing a PrototypeFactory.
	prototypesCtx = &ctxapi.Key{Name: "process.prototypes"}

	// hostsCtx is the context key for storing a HostRegistry.
	hostsCtx = &ctxapi.Key{Name: "process.hosts"}

	// onCompleteCtx is the context key for storing process completion callbacks (ScopeFrame: call-specific).
	onCompleteCtx = &ctxapi.Key{Name: "process.onComplete"}

	// onStartCtx is the context key for storing process start callbacks (ScopeFrame: call-specific).
	onStartCtx = &ctxapi.Key{Name: "process.onStart"}
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
type OnComplete func(pid relay.PID, result *runtime.Result)

// OnStart is the type for a process start callback.
// It is called when a process begins execution.
// The callback receives the process ID and the process instance.
type OnStart func(pid relay.PID, proc Process)

// SetOnComplete sets an OnComplete callback in the FrameContext.
// If there's already one present, it combines them so that both are called.
// This enables composable process lifecycle management where multiple
// components can register callbacks for the same lifecycle events.
// The most recently added callback will be called first.
// Returns error if no frame context or frame is sealed.
func SetOnComplete(ctx context.Context, cb OnComplete) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return errors.New("no frame context available")
	}

	if val, ok := fc.Get(onCompleteCtx); ok {
		if existing, ok := val.(OnComplete); ok {
			combined := func(pid relay.PID, result *runtime.Result) {
				cb(pid, result)
				existing(pid, result)
			}
			return fc.Set(onCompleteCtx, OnComplete(combined))
		}
	}

	return fc.Set(onCompleteCtx, cb)
}

// SetOnStart sets an OnStart callback in the FrameContext.
// If there's already one present, it combines them so that both are called.
// This enables composable process lifecycle management where multiple
// components can register callbacks for the same lifecycle events.
// The most recently added callback will be called first.
// Returns error if no frame context or frame is sealed.
func SetOnStart(ctx context.Context, cb OnStart) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return errors.New("no frame context available")
	}

	if val, ok := fc.Get(onStartCtx); ok {
		if existing, ok := val.(OnStart); ok {
			combined := func(pid relay.PID, proc Process) {
				cb(pid, proc)
				existing(pid, proc)
			}
			return fc.Set(onStartCtx, OnStart(combined))
		}
	}

	return fc.Set(onStartCtx, cb)
}

// GetOnComplete retrieves the OnComplete callback from the FrameContext.
// Returns nil if no callback is found.
// This is typically used by process supervisors to get the callback
// that should be invoked when a process completes.
func GetOnComplete(ctx context.Context) OnComplete {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(onCompleteCtx); ok {
		if cb, ok := val.(OnComplete); ok {
			return cb
		}
	}
	return nil
}

// GetOnStart retrieves the OnStart callback from the FrameContext.
// Returns nil if no callback is found.
// This is typically used by process hosts to get the callback
// that should be invoked when a process starts.
func GetOnStart(ctx context.Context) OnStart {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(onStartCtx); ok {
		if cb, ok := val.(OnStart); ok {
			return cb
		}
	}
	return nil
}
