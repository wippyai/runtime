package boot

import (
	"context"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/wildcard"
	"github.com/ponyruntime/pony/system/eventbus"
)

// HandlerRegistry collects event handlers during plugin initialization.
type HandlerRegistry interface {
	// Register registers an event handler for a specific pattern
	Register(handler eventbus.EventHandler)

	// RegisterListener registers a registry.EntryListener for matching registry events
	RegisterListener(kinds registry.Kind, listener registry.EntryListener)

	// Handlers returns all registered handlers
	Handlers() []eventbus.EventHandler
}

type handlerRegistry struct {
	handlers []eventbus.EventHandler
}

// NewHandlerRegistry creates a new handler registry.
func NewHandlerRegistry() HandlerRegistry {
	return &handlerRegistry{
		handlers: make([]eventbus.EventHandler, 0),
	}
}

func (r *handlerRegistry) Register(handler eventbus.EventHandler) {
	r.handlers = append(r.handlers, handler)
}

func (r *handlerRegistry) RegisterListener(kinds registry.Kind, listener registry.EntryListener) {
	handler := wrapListener(kinds, listener)
	r.handlers = append(r.handlers, handler)
}

func (r *handlerRegistry) Handlers() []eventbus.EventHandler {
	return r.handlers
}

// wrapListener wraps a registry.EntryListener into an eventbus.EventHandler.
// This is the boot-level equivalent of system/registry/events/handler.go.
func wrapListener(kinds registry.Kind, listener registry.EntryListener) eventbus.EventHandler {
	w := wildcard.NewWildcard(kinds)

	return eventbus.NewBaseHandler(
		eventbus.Pattern{System: registry.System, Kind: registry.AllEvents},
		func(ctx context.Context, evt event.Event) error {
			entry, ok := evt.Data.(registry.Entry)
			if !ok {
				// Handle transaction events
				switch evt.Kind {
				case registry.Begin:
					if tx, ok := listener.(registry.TransactionListener); ok {
						tx.Begin(ctx)
					}
					return nil
				case registry.Commit:
					if tx, ok := listener.(registry.TransactionListener); ok {
						tx.Commit(ctx)
					}
					return nil
				case registry.Discard:
					if tx, ok := listener.(registry.TransactionListener); ok {
						tx.Discard(ctx)
					}
					return nil
				}
				return nil
			}

			if !w.Match(entry.Kind) {
				return nil
			}

			var err error
			switch evt.Kind {
			case registry.Create:
				err = listener.Add(ctx, entry)
			case registry.Update:
				err = listener.Update(ctx, entry)
			case registry.Delete:
				err = listener.Delete(ctx, entry)
			}

			return err
		},
	)
}

type handlerRegistryKey struct{}

// WithHandlerRegistry stores the handler registry in the context.
func WithHandlerRegistry(ctx context.Context, registry HandlerRegistry) context.Context {
	return context.WithValue(ctx, handlerRegistryKey{}, registry)
}

// GetHandlerRegistry retrieves the handler registry from the context.
func GetHandlerRegistry(ctx context.Context) HandlerRegistry {
	if registry, ok := ctx.Value(handlerRegistryKey{}).(HandlerRegistry); ok {
		return registry
	}
	return nil
}
