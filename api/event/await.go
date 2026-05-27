// SPDX-License-Identifier: MPL-2.0

package event

import (
	"context"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// DefaultAwaitTimeout is the default request/reply wait budget used when a
// caller passes a non-positive timeout to an AwaitService.
const DefaultAwaitTimeout = 30 * time.Second

var awaitServiceKey = &ctxapi.Key{Name: "event.await"}

// AwaitResult contains the result of waiting for an event.
type AwaitResult struct {
	Error    error
	Event    Event
	Accepted bool
}

// AwaitWaiter represents a prepared wait operation.
// Call Wait after triggering the request event.
// Close can be used to cancel/cleanup without waiting.
type AwaitWaiter interface {
	Wait() AwaitResult
	Close()
}

// AwaitService provides request-response pattern over pub-sub.
// It maintains a single subscription per (system, kind) pair and routes
// events by path to waiting callers, avoiding channel overflow issues
// that occur with multiple independent subscriptions.
type AwaitService interface {
	// Prepare registers a waiter before the triggering request is sent.
	// This avoids reply races where response arrives before wait registration.
	// A non-positive timeout uses DefaultAwaitTimeout.
	Prepare(ctx context.Context, system System, kind Kind, path Path, timeout time.Duration) (AwaitWaiter, error)

	// Await waits for an event matching system, kind, and path.
	// Returns when matching event arrives or timeout expires.
	// A non-positive timeout uses DefaultAwaitTimeout.
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
