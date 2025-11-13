// Package pubsub provides a publish-subscribe messaging system for inter-component communication.
package pubsub

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// Context keys for storing pubsub-related data in context
var (
	// nodeCtx is used to store the Node instance in context (ScopeThread: inherited, mutable)
	nodeCtx        = &ctxapi.Key{Name: "pubsub.node"}
	routerCtx      = &ctxapi.Key{Name: "pubsub.router"}
	nodeManagerCtx = &ctxapi.Key{Name: "pubsub.nodemanager"}
	hostCtx        = &ctxapi.Key{Name: "pubsub.host"}
)

// NodeManager manages pubsub nodes and hosts.
type NodeManager interface {
	Node() Node
	Start(ctx context.Context) error
	Stop() error
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

// WithNodeManager attaches a NodeManager instance to the provided context.
func WithNodeManager(ctx context.Context, nm NodeManager) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(nodeManagerCtx) == nil {
		ac.With(nodeManagerCtx, nm)
	}
	return ctx
}

// GetNodeManager retrieves the NodeManager instance from the provided context.
func GetNodeManager(ctx context.Context) NodeManager {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(nodeManagerCtx); val != nil {
		if nm, ok := val.(NodeManager); ok {
			return nm
		}
	}
	return nil
}

// WithHost attaches a Host instance to the provided context at app level.
// This is for storing service-level hosts, not frame-level hosts.
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

// GetHost retrieves the Host instance from the provided context at app level.
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
