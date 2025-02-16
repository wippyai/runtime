package registry

import (
	"context"
	"errors"
	"fmt"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/wildcard"
	"github.com/ponyruntime/pony/system/eventbus"
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
		r.defaultListener = l
		// Check if the listener supports transactions
		if _, ok := l.(registry.TransactionListener); ok {
			r.defaultTransactional = true
		}
	}
}

// WithListener adds a new kind-based routing rule
func WithListener(pattern string, listener registry.EntryListener) Option {
	return func(r *Router) {
		isTransactional := false
		if _, ok := listener.(registry.TransactionListener); ok {
			isTransactional = true
		}
		r.routes = append(r.routes, route{
			pattern:       pattern,
			listener:      listener,
			wildcard:      wildcard.NewWildcard(pattern),
			transactional: isTransactional,
		})
	}
}

// route represents a single routing rule
type route struct {
	pattern       string
	listener      registry.EntryListener
	wildcard      *wildcard.Wildcard
	transactional bool
}

// Router handles registry events and routes them to appropriate listeners
type Router struct {
	ctx                  context.Context
	log                  *zap.Logger
	bus                  events.Bus
	subscriber           *eventbus.Subscriber
	routes               []route
	defaultListener      registry.EntryListener
	defaultTransactional bool
}

// NewRouter creates a new Router instance with the provided options
func NewRouter(ctx context.Context, bus events.Bus, opts ...Option) (*Router, error) {
	r := &Router{
		ctx: ctx,
		bus: bus,
		log: zap.NewNop(),
	}

	// Apply options
	for _, opt := range opts {
		opt(r)
	}

	// Log detected transaction listeners for debugging
	for _, route := range r.routes {
		if route.transactional {
			r.log.Debug("detected transactional listener",
				zap.String("pattern", route.pattern))
		}
	}

	if r.defaultListener != nil && r.defaultTransactional {
		r.log.Debug("detected transactional default listener")
	}

	// Subscribe to all registry changes
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		registry.System,
		registry.AllEvents,
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
	// Handle transaction events
	switch evt.Kind {
	case registry.Begin:
		r.handleTransactionBegin()
		return
	case registry.Commit:
		r.handleTransactionCommit()
		return
	case registry.Discard:
		r.handleTransactionDiscard()
		return
	}

	// Handle regular entry events
	if evt.Data == nil {
		r.log.Debug("ignoring empty registry event", zap.Any("event", evt))
		return
	}

	entry, ok := evt.Data.(registry.Entry)
	if !ok {
		r.log.Warn("invalid registry event data", zap.Any("event", evt))
		return
	}

	r.log.Debug("processing registry event",
		zap.String("id", entry.ID.String()),
		zap.String("kind", evt.Kind),
		zap.String("type", entry.Kind))

	if evt.Kind != registry.Delete && entry.Data == nil {
		r.reject(entry.ID, fmt.Errorf("configuration data is required for create/update operations"))
		return
	}

	listener := r.findListener(entry.Kind)
	if listener == nil {
		r.log.Debug("no listener found for entry kind", zap.String("kind", string(entry.Kind)))
		return
	}

	if err := r.processWithListener(evt.Kind, entry, listener); err != nil {
		if errors.Is(err, ErrSkipOperation) {
			return
		}
		r.reject(entry.ID, err)
		return
	}

	r.accept(entry.ID)
}

func (r *Router) handleTransactionBegin() {
	r.log.Debug("handling transaction begin")

	// Notify all transactional listeners
	for _, route := range r.routes {
		if route.transactional {
			if txListener, ok := route.listener.(registry.TransactionListener); ok {
				txListener.Begin(r.ctx)
			}
		}
	}

	// Check default listener
	if r.defaultTransactional {
		if txListener, ok := r.defaultListener.(registry.TransactionListener); ok {
			txListener.Begin(r.ctx)
		}
	}
}

func (r *Router) handleTransactionCommit() {
	r.log.Debug("handling transaction commit")

	// Notify all transactional listeners
	for _, route := range r.routes {
		if route.transactional {
			if txListener, ok := route.listener.(registry.TransactionListener); ok {
				txListener.Commit(r.ctx)
			}
		}
	}

	// Check default listener
	if r.defaultTransactional {
		if txListener, ok := r.defaultListener.(registry.TransactionListener); ok {
			txListener.Commit(r.ctx)
		}
	}
}

func (r *Router) handleTransactionDiscard() {
	r.log.Debug("handling transaction discard")

	// Notify all transactional listeners
	for _, route := range r.routes {
		if route.transactional {
			if txListener, ok := route.listener.(registry.TransactionListener); ok {
				txListener.Discard(r.ctx)
			}
		}
	}

	// Check default listener
	if r.defaultTransactional {
		if txListener, ok := r.defaultListener.(registry.TransactionListener); ok {
			txListener.Discard(r.ctx)
		}
	}
}

func (r *Router) findListener(kind registry.Kind) registry.EntryListener {
	for _, route := range r.routes {
		if route.wildcard.Match(string(kind)) {
			return route.listener
		}
	}
	return r.defaultListener
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
		Path:   id.String(),
	})
}

func (r *Router) reject(id registry.ID, err error) {
	r.bus.Send(r.ctx, events.Event{
		System: registry.System,
		Kind:   registry.Reject,
		Path:   id.String(),
		Data:   err,
	})
}
