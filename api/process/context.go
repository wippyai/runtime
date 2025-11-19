// Package process provides process abstraction and lifecycle.
package process

import (
	"context"
	"errors"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process/stats"
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
	stats.Aggregator
}

// Context keys for storing process-related information in context.Context
var (
	// managerCtx is the context key for storing a process Manager.
	managerCtx = &ctxapi.Key{Name: "process.manager"}

	// prototypesCtx is the context key for storing a PrototypeFactory.
	prototypesCtx = &ctxapi.Key{Name: "process.prototypes"}

	// hostsCtx is the context key for storing a HostRegistry.
	hostsCtx = &ctxapi.Key{Name: "process.hosts"}

	// onCompleteHooksCtx is the context key for storing process completion hook arrays (ScopeFrame: call-specific).
	onCompleteHooksCtx = &ctxapi.Key{Name: "process.onCompleteHooks"}

	// onStartHooksCtx is the context key for storing process start hook arrays (ScopeFrame: call-specific).
	onStartHooksCtx = &ctxapi.Key{Name: "process.onStartHooks"}
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

// SetOnStartHooks sets OnStart hook arrays in the FrameContext.
// Replaces any existing hooks.
// Returns error if no frame context or frame is sealed.
func SetOnStartHooks(ctx context.Context, hooks []OnStart) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return errors.New("no frame context available")
	}
	return fc.Set(onStartHooksCtx, hooks)
}

// GetOnStartHooks retrieves the OnStart hook array from the FrameContext.
// Returns nil if no hooks are found.
// This is typically used by process hosts to get the hooks
// that should be invoked when a process starts.
func GetOnStartHooks(ctx context.Context) []OnStart {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(onStartHooksCtx); ok {
		if hooks, ok := val.([]OnStart); ok {
			return hooks
		}
	}
	return nil
}

// SetOnCompleteHooks sets OnComplete hook arrays in the FrameContext.
// Replaces any existing hooks.
// Returns error if no frame context or frame is sealed.
func SetOnCompleteHooks(ctx context.Context, hooks []OnComplete) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return errors.New("no frame context available")
	}
	return fc.Set(onCompleteHooksCtx, hooks)
}

// GetOnCompleteHooks retrieves the OnComplete hook array from the FrameContext.
// Returns nil if no hooks are found.
// This is typically used by process supervisors to get the hooks
// that should be invoked when a process completes.
func GetOnCompleteHooks(ctx context.Context) []OnComplete {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(onCompleteHooksCtx); ok {
		if hooks, ok := val.([]OnComplete); ok {
			return hooks
		}
	}
	return nil
}
