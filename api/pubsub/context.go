package pubsub

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// Context keys for storing pubsub-related data in context
var (
	// pidCtx is used to store process identifier in context (ScopeCall: write-once, call-specific)
	pidCtx = &ctxapi.Key{Name: "pubsub.pid", Scope: ctxapi.ScopeCall}

	// nodeCtx is used to store the Node instance in context (ScopeThread: inherited, mutable)
	nodeCtx   = &ctxapi.Key{Name: "pubsub.node", Scope: ctxapi.ScopeThread}
	hostCtx   = &ctxapi.Key{Name: "pubsub.host", Scope: ctxapi.ScopeThread}
	routerCtx = &ctxapi.Key{Name: "pubsub.router", Scope: ctxapi.ScopeThread}
)

// WithPID attaches a process identifier (PID) to the provided context.
// This allows the PID to be retrieved later using the GetPID function.
func WithPID(ctx context.Context, process PID) context.Context {
	return context.WithValue(ctx, pidCtx, process)
}

// GetPID retrieves the process identifier (PID) from the provided context.
// Returns the PID and a boolean indicating if it was found.
func GetPID(ctx context.Context) (PID, bool) {
	p, ok := ctx.Value(pidCtx).(PID)
	if !ok {
		return PID{}, false
	}
	return p, true
}

// WithNode attaches a Node instance to the provided context.
// This allows the Node to be retrieved later using the GetNode function.
func WithNode(ctx context.Context, node Node) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(nodeCtx) == nil {
		ac.With(nodeCtx, node)
	}
	return ctx
}

// GetNode retrieves the Node instance from the provided context.
// Returns nil if no Node is found in the context.
func GetNode(ctx context.Context) Node {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(nodeCtx); val != nil {
		if n, ok := val.(Node); ok {
			return n
		}
	}
	return nil
}

// WithHost attaches a Host instance to the provided context.
// This allows the Host to be retrieved later using the GetHost function.
func WithHost(ctx context.Context, host Host) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(hostCtx) == nil {
		ac.With(hostCtx, host)
	}
	return ctx
}

// GetHost retrieves the Host instance from the provided context.
// Returns nil if no Host is found in the context.
func GetHost(ctx context.Context) Host {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(hostCtx); val != nil {
		if h, ok := val.(Host); ok {
			return h
		}
	}
	return nil
}

// WithRouter attaches a Receiver instance to the provided context.
// This allows the Router to be retrieved later using the GetRouter function.
func WithRouter(ctx context.Context, r Receiver) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(routerCtx) == nil {
		ac.With(routerCtx, r)
	}
	return ctx
}

// GetRouter retrieves the Receiver (router) from the provided context.
// Returns nil if no Router is found in the context.
func GetRouter(ctx context.Context) Receiver {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(routerCtx); val != nil {
		if r, ok := val.(Receiver); ok {
			return r
		}
	}
	return nil
}
