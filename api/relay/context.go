package relay

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var (
	nodeCtxKey        = &ctxapi.Key{Name: "relay.nodeCtxKey"}
	routerCtxKey      = &ctxapi.Key{Name: "relay.routerCtxKey"}
	nodeManagerCtxKey = &ctxapi.Key{Name: "relay.nodeManagerCtxKey"}
	hostCtxKey        = &ctxapi.Key{Name: "relay.hostCtxKey"}
)

// WithNode attaches a Node to the context.
func WithNode(ctx context.Context, node Node) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(nodeCtxKey) == nil {
		ac.With(nodeCtxKey, node)
	}
	return ctx
}

// GetNode retrieves the Node from the context.
func GetNode(ctx context.Context) Node {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(nodeCtxKey); val != nil {
		if n, ok := val.(Node); ok {
			return n
		}
	}
	return nil
}

// WithRouter attaches a Receiver to the context.
func WithRouter(ctx context.Context, r Receiver) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(routerCtxKey) == nil {
		ac.With(routerCtxKey, r)
	}
	return ctx
}

// GetRouter retrieves the Receiver from the context.
func GetRouter(ctx context.Context) Receiver {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(routerCtxKey); val != nil {
		if r, ok := val.(Receiver); ok {
			return r
		}
	}
	return nil
}

// WithNodeManager attaches a NodeManager to the context.
func WithNodeManager(ctx context.Context, nm NodeManager) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(nodeManagerCtxKey) == nil {
		ac.With(nodeManagerCtxKey, nm)
	}
	return ctx
}

// GetNodeManager retrieves the NodeManager from the context.
func GetNodeManager(ctx context.Context) NodeManager {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(nodeManagerCtxKey); val != nil {
		if nm, ok := val.(NodeManager); ok {
			return nm
		}
	}
	return nil
}

// WithHost attaches a Host to the context.
func WithHost(ctx context.Context, host Host) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(hostCtxKey) == nil {
		ac.With(hostCtxKey, host)
	}
	return ctx
}

// GetHost retrieves the Host from the context.
func GetHost(ctx context.Context) Host {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(hostCtxKey); val != nil {
		if h, ok := val.(Host); ok {
			return h
		}
	}
	return nil
}
