// SPDX-License-Identifier: MPL-2.0

// Package eventbus provides a routing mechanism for handling events.
package eventbus

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"go.uber.org/zap"
)

// EventHandler defines an interface for handling events.
// Implementations of this interface can be registered with an EventRouter
// to receive events matching their pattern.
type EventHandler interface {
	// Pattern returns the event matching criteria for this handler
	Pattern() Pattern
	// Handle processes an event that matches the pattern
	// Returns an error if the handling fails
	Handle(context.Context, event.Event) error
}

// Pattern defines the matching criteria for events.
// It matches events by their system and kind identifiers.
type Pattern struct {
	// System identifies the system category of events to match
	System event.System
	// Kind identifies the specific kind of events to match
	Kind event.Kind
}

// BaseHandler provides a basic implementation of EventHandler.
// It simplifies creation of event handlers with a function-based approach.
type BaseHandler struct {
	handler func(context.Context, event.Event) error
	pattern Pattern
}

// NewBaseHandler creates a new handler with the specified pattern and handler function.
// This is a convenience function for creating simple event handlers.
func NewBaseHandler(pattern Pattern, handler func(context.Context, event.Event) error) *BaseHandler {
	return &BaseHandler{
		pattern: pattern,
		handler: handler,
	}
}

// Pattern returns the event matching criteria for this handler.
func (h *BaseHandler) Pattern() Pattern {
	return h.pattern
}

// Handle processes an event by delegating to the handler function.
func (h *BaseHandler) Handle(ctx context.Context, evt event.Event) error {
	return h.handler(ctx, evt)
}

// handlerSubscription represents a registered event handler and its associated subscriber.
type handlerSubscription struct {
	handler    EventHandler
	subscriber *Subscriber
}

// EventRouter distributes events from an event bus to registered handlers.
// It manages subscriptions and handler lifecycle, ensuring events are properly
// delivered to the appropriate handlers.
type EventRouter struct {
	ctx         context.Context
	cancel      context.CancelFunc
	bus         event.Bus
	log         *zap.Logger
	subscribers []handlerSubscription
	mu          sync.RWMutex
}

// RouterOption defines a function that configures an EventRouter.
// These options are used with StartRouter to customize router behavior.
type RouterOption func(*EventRouter)

// WithLogger sets a custom logger for the EventRouter.
// This allows integration with application-wide logging systems.
func WithLogger(log *zap.Logger) RouterOption {
	return func(r *EventRouter) {
		r.log = log
	}
}

// WithHandlers registers initial event handlers with the router.
// This is a convenience option for setting up handlers during router creation.
func WithHandlers(handlers ...EventHandler) RouterOption {
	return func(r *EventRouter) {
		for _, h := range handlers {
			if err := r.addHandler(h); err != nil {
				r.log.Error("failed to add initial handler", zap.Error(err))
			}
		}
	}
}

// StartRouter creates and initializes a new EventRouter with the provided options.
// The router will be attached to the specified event bus and context.
// Returns the initialized router and any error that occurred during setup.
func StartRouter(ctx context.Context, bus event.Bus, opts ...RouterOption) (*EventRouter, error) {
	ctx, cancel := context.WithCancel(ctx)

	r := &EventRouter{
		ctx:         ctx,
		cancel:      cancel,
		bus:         bus,
		log:         zap.NewNop(),
		subscribers: make([]handlerSubscription, 0),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r, nil
}

// AddHandler registers a new handler with the router.
func (r *EventRouter) AddHandler(h EventHandler) error {
	return r.addHandler(h)
}

// Stop gracefully shuts down the router and all its subscriptions.
// It cancels the internal context and waits for all subscribers to close.
// Returns any error encountered during shutdown.
func (r *EventRouter) Stop() error {
	r.cancel()

	// Spawn WaitGroup with initial count of subscribers
	r.mu.RLock()
	wg := sync.WaitGroup{}
	wg.Add(len(r.subscribers))

	// close all subscribers concurrently
	for _, sub := range r.subscribers {
		go func(s handlerSubscription) {
			defer wg.Done()
			s.subscriber.Close()
		}(sub)
	}
	r.mu.RUnlock()

	// Wait for all subscribers to close
	wg.Wait()

	// Clear subscribers list after all are closed
	r.mu.Lock()
	r.subscribers = nil
	r.mu.Unlock()

	return nil
}

// addHandler registers a new event handler with the router.
// It creates a subscription to the event bus for the handler's pattern.
// Returns an error if the subscription fails or the router is stopped.
func (r *EventRouter) addHandler(h EventHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check context before proceeding
	if r.ctx.Err() != nil {
		return NewRouterCanceledError(r.ctx.Err())
	}

	pattern := h.Pattern()
	sb, err := NewSubscriber(r.ctx, r.bus, pattern.System, pattern.Kind,
		func(e event.Event) {
			if err := h.Handle(r.ctx, e); err != nil {
				r.log.Error("failed to handle event", zap.Error(err))
			}
		},
	)

	if err != nil {
		return NewSubscriberError(err)
	}

	r.subscribers = append(r.subscribers, handlerSubscription{
		handler:    h,
		subscriber: sb,
	})

	return nil
}
