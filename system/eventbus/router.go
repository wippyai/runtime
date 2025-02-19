package eventbus

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"go.uber.org/zap"
)

// EventHandler defines an interface for handling events
type EventHandler interface {
	Pattern() Pattern
	Handle(context.Context, events.Event) error
}

// Pattern defines the matching criteria for events
type Pattern struct {
	System events.System
	Kind   events.Kind
}

// BaseHandler provides a basic implementation of EventHandler
type BaseHandler struct {
	pattern Pattern
	handler func(context.Context, events.Event) error
}

func NewBaseHandler(pattern Pattern, handler func(context.Context, events.Event) error) *BaseHandler {
	return &BaseHandler{
		pattern: pattern,
		handler: handler,
	}
}

func (h *BaseHandler) Pattern() Pattern {
	return h.pattern
}

func (h *BaseHandler) Handle(ctx context.Context, evt events.Event) error {
	return h.handler(ctx, evt)
}

type handlerSubscription struct {
	handler    EventHandler
	subscriber *Subscriber
}

type EventRouter struct {
	ctx         context.Context
	cancel      context.CancelFunc
	bus         events.Bus
	log         *zap.Logger
	subscribers []handlerSubscription
	mu          sync.RWMutex
}

type RouterOption func(*EventRouter)

func WithLogger(log *zap.Logger) RouterOption {
	return func(r *EventRouter) {
		r.log = log
	}
}

func WithHandlers(handlers ...EventHandler) RouterOption {
	return func(r *EventRouter) {
		for _, h := range handlers {
			if err := r.addHandler(h); err != nil {
				r.log.Error("failed to add initial handler", zap.Error(err))
			}
		}
	}
}

func StartRouter(ctx context.Context, bus events.Bus, opts ...RouterOption) (*EventRouter, error) {
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

func (r *EventRouter) Stop() error {
	r.cancel()

	// Spawn WaitGroup with initial count of subscribers
	r.mu.RLock()
	wg := sync.WaitGroup{}
	wg.Add(len(r.subscribers))

	// Close all subscribers concurrently
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

func (r *EventRouter) addHandler(h EventHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check context before proceeding
	if r.ctx.Err() != nil {
		return fmt.Errorf("router context canceled: %w", r.ctx.Err())
	}

	pattern := h.Pattern()
	sub, err := NewSubscriber(r.ctx, r.bus, pattern.System, pattern.Kind,
		func(evt events.Event) {
			if err := h.Handle(r.ctx, evt); err != nil {
				r.log.Error("failed to handle event", zap.Error(err))
			}
		},
	)

	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}

	r.subscribers = append(r.subscribers, handlerSubscription{
		handler:    h,
		subscriber: sub,
	})

	return nil
}
