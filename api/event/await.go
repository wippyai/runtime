package event

import (
	"context"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var awaitServiceKey = &ctxapi.Key{Name: "event.await"}

// AwaitResult contains the result of waiting for an event.
type AwaitResult struct {
	Error    error
	Event    Event
	Accepted bool
}

// AwaitService provides request-response pattern over pub-sub.
// It maintains a single subscription per (system, kind) pair and routes
// events by path to waiting callers, avoiding channel overflow issues
// that occur with multiple independent subscriptions.
type AwaitService interface {
	// Await waits for an event matching system, kind, and path.
	// Returns when matching event arrives or timeout expires.
	Await(ctx context.Context, system System, kind Kind, path Path, timeout time.Duration) AwaitResult

	// Start initializes the service.
	Start(ctx context.Context) error

	// Stop shuts down the service.
	Stop() error
}

// WithAwaitService attaches an AwaitService to the context.
func WithAwaitService(ctx context.Context, svc AwaitService) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(awaitServiceKey) == nil {
		ac.With(awaitServiceKey, svc)
	}
	return ctx
}

// GetAwaitService retrieves the AwaitService from the context.
func GetAwaitService(ctx context.Context) AwaitService {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if s := ac.Get(awaitServiceKey); s != nil {
		return s.(AwaitService)
	}
	return nil
}
