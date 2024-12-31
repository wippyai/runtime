package registry

import (
	"context"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/wildcard"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"go.uber.org/zap"
)

// ErrSkipOperation is a special error type that indicates the operation should be skipped
// without triggering a reject event
var ErrSkipOperation = errors.New("skip operation")

// Option configures a Router
type Option func(*Router)

// WithLogger sets the logger for the router
func WithLogger(log *zap.Logger) Option {
	return func(r *Router) {
		r.log = log
	}
}

// WithDefaultListener sets the default listener for unmatched events
func WithDefaultListener(l registry.EntryListener) Option {
	return func(r *Router) {
		r.default_ = l
	}
}

// WithListener adds a new kind-based routing rule
func WithListener(pattern string, listener registry.EntryListener) Option {
	return func(r *Router) {
		r.routes = append(r.routes, route{
			pattern:  pattern,
			listener: listener,
			wildcard: wildcard.NewWildcard(pattern),
		})
	}
}

// route represents a single routing rule
type route struct {
	pattern  string
	listener registry.EntryListener
	wildcard *wildcard.Wildcard
}

// Router handles registry events and routes them to appropriate listeners
type Router struct {
	ctx        context.Context
	log        *zap.Logger
	bus        events.Bus
	subscriber *eventbus.Subscriber
	routes     []route
	default_   registry.EntryListener
}

// NewRouter creates a new Router instance with the provided options
func NewRouter(ctx context.Context, bus events.Bus, opts ...Option) (*Router, error) {
	r := &Router{
		ctx: ctx,
		bus: bus,
		log: zap.NewNop(), // Default no-op logger
	}

	// Apply options
	for _, opt := range opts {
		opt(r)
	}

	// Subscribe to all registry changes
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		registry.System,
		registry.Changes,
		r.handleEvent,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create subscriber: %w", err)
	}
	r.subscriber = sub

	return r, nil
}

// Stop gracefully shuts down the router
func (r *Router) Stop() error {
	if r.subscriber != nil {
		r.subscriber.Close()
	}
	return nil
}

func (r *Router) handleEvent(evt events.Event) {
	entry, ok := evt.Data.(registry.Entry)
	if !ok {
		r.log.Warn("invalid registry event data", zap.Any("event", evt))
		return
	}

	r.log.Debug("processing registry event",
		zap.String("id", string(entry.ID)),
		zap.String("kind", string(evt.Kind)),
		zap.String("type", string(entry.Kind)))

	// For create/update operations, ensure we have valid data
	if evt.Kind != registry.Delete && entry.Data == nil {
		r.reject(entry.ID, fmt.Errorf("configuration data is required for create/update operations"))
		return
	}

	// Find matching listener
	listener := r.findListener(entry.Kind)
	if listener == nil {
		r.log.Debug("no listener found for entry kind", zap.String("kind", string(entry.Kind)))
		return
	}

	// Process with found listener
	if err := r.processWithListener(evt.Kind, entry, listener); err != nil {
		if errors.Is(err, ErrSkipOperation) {
			return
		}
		r.reject(entry.ID, err)
		return
	}

	r.accept(entry.ID)
}

// findListener returns the first matching listener for the given kind
func (r *Router) findListener(kind registry.Kind) registry.EntryListener {
	// Try to match routes
	for _, route := range r.routes {
		if route.wildcard.Match(string(kind)) {
			return route.listener
		}
	}

	// Return default listener if no routes matched
	return r.default_
}

func (r *Router) processWithListener(kind events.Kind, entry registry.Entry, listener registry.EntryListener) error {
	switch kind {
	case registry.Create:
		return listener.Add(r.ctx, entry)
	case registry.Update:
		return listener.Update(r.ctx, entry)
	case registry.Delete:
		return listener.Delete(r.ctx, entry)
	default:
		return fmt.Errorf("unsupported operation kind: %s", kind)
	}
}

func (r *Router) accept(id registry.ID) {
	r.bus.Send(r.ctx, events.Event{
		System: registry.System,
		Kind:   registry.Accept,
		Path:   events.Path(id),
	})
}

func (r *Router) reject(id registry.ID, err error) {
	r.bus.Send(r.ctx, events.Event{
		System: registry.System,
		Kind:   registry.Reject,
		Path:   events.Path(id),
		Data:   err,
	})
}
