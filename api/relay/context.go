package relay

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var (
	nodeKey        = &ctxapi.Key{Name: "relay.node"}
	routerKey      = &ctxapi.Key{Name: "relay.router"}
	nodeManagerKey = &ctxapi.Key{Name: "relay.node_manager"}
	hostKey        = &ctxapi.Key{Name: "relay.host"}
)

// WithNode attaches a Node to the context.
func WithNode(ctx context.Context, node Node) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(nodeKey) == nil {
		ac.With(nodeKey, node)
	}
	return ctx
}

// GetNode retrieves the Node from the context.
func GetNode(ctx context.Context) Node {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(nodeKey); val != nil {
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
	if ac.Get(routerKey) == nil {
		ac.With(routerKey, r)
	}
	return ctx
}

// GetRouter retrieves the Receiver from the context.
func GetRouter(ctx context.Context) Receiver {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(routerKey); val != nil {
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
	if ac.Get(nodeManagerKey) == nil {
		ac.With(nodeManagerKey, nm)
	}
	return ctx
}

// GetNodeManager retrieves the NodeManager from the context.
func GetNodeManager(ctx context.Context) NodeManager {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(nodeManagerKey); val != nil {
		if nm, ok := val.(NodeManager); ok {
			return nm
		}
	}
	return nil
}

// WithHost attaches a Receiver to the context.
func WithHost(ctx context.Context, host Receiver) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(hostKey) == nil {
		ac.With(hostKey, host)
	}
	return ctx
}

// GetHost retrieves the Receiver from the context.
func GetHost(ctx context.Context) Receiver {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(hostKey); val != nil {
		if h, ok := val.(Receiver); ok {
			return h
		}
	}
	return nil
}
