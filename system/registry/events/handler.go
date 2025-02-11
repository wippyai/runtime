package events

import (
	"context"
	"errors"
	"fmt"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/wildcard"
	"github.com/ponyruntime/pony/system/eventbus"
)

// ErrSkipOperation is a special error type that indicates the operation should be skipped
// without triggering a reject event
var ErrSkipOperation = errors.New("skip operation")

// WithRegistryHandler adapts a registry listener with pattern matching to event router
func WithRegistryHandler(kinds registry.Kind, listener registry.EntryListener) eventbus.EventHandler {
	w := wildcard.NewWildcard(kinds)

	return eventbus.NewBaseHandler(
		eventbus.Pattern{System: registry.System, Kind: registry.AllEvents},
		func(ctx context.Context, evt events.Event) error {
			bus := ctx.Value(contextapi.BusCtx).(events.Bus)
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

				// Skip if not a registry entry event
				return nil
			}

			// Pattern match on entry.Kind
			if !w.Match(entry.Kind) {
				return nil
			}

			// Validate data for non-delete operations
			if evt.Kind != registry.Delete && entry.Data == nil {
				reject(ctx, bus, entry.ID, fmt.Errorf("configuration data is required for create/update operations"))
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
			default:
				err = fmt.Errorf("unknown event kind: %s", evt.Kind)
			}

			if err != nil {
				if errors.Is(err, ErrSkipOperation) {
					return nil
				}
				reject(ctx, bus, entry.ID, err)
				return nil
			}

			accept(ctx, bus, entry.ID)
			return nil
		},
	)
}

func accept(ctx context.Context, bus events.Bus, id registry.ID) {
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Accept,
		Path:   id.String(),
	})
}

func reject(ctx context.Context, bus events.Bus, id registry.ID, err error) {
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Reject,
		Path:   id.String(),
		Data:   err,
	})
}
