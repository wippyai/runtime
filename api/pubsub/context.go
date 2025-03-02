// Package pubsub provides a publish-subscribe messaging system for inter-component communication.
package pubsub

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
)

// Context keys for storing pubsub-related data in context
var (
	// pidCtx is used to store process identifier in context
	pidCtx = &ctxapi.Key{Name: "pubsub.pid"} //nolint:gochecknoglobals

	// nodeCtx is used to store the Node instance in context
	nodeCtx = &ctxapi.Key{Name: "pubsub.node"} //nolint:gochecknoglobals
	// hostCtx is used to store the Host instance in context
	hostCtx = &ctxapi.Key{Name: "pubsub.host"} //nolint:gochecknoglobals
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
	return context.WithValue(ctx, nodeCtx, node)
}

// GetNode retrieves the Node instance from the provided context.
// Returns nil if no Node is found in the context.
func GetNode(ctx context.Context) Node {
	if n, ok := ctx.Value(nodeCtx).(Node); ok {
		return n
	}
	return nil
}

// WithHost attaches a Host instance to the provided context.
// This allows the Host to be retrieved later using the GetHost function.
func WithHost(ctx context.Context, host Host) context.Context {
	return context.WithValue(ctx, hostCtx, host)
}

// GetHost retrieves the Host instance from the provided context.
// Returns nil if no Host is found in the context.
func GetHost(ctx context.Context) Host {
	if h, ok := ctx.Value(hostCtx).(Host); ok {
		return h
	}
	return nil
}
