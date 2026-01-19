package events

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/wildcard"
	"github.com/wippyai/runtime/system/eventbus"
)

// ErrSkipOperation is a special error type that indicates the operation should be skipped
// without triggering a reject event
var ErrSkipOperation = errors.New("skip operation")

// NewRegistryHandler adapts a registry listener with pattern matching to event router
func NewRegistryHandler(kinds registry.Kind, listener registry.EntryListener) eventbus.EventHandler {
	w := wildcard.NewWildcard(kinds)

	return eventbus.NewBaseHandler(
		eventbus.Pattern{System: registry.System, Kind: registry.AllEvents},
		func(ctx context.Context, evt event.Event) error {
			bus := event.GetBus(ctx)
			entry, ok := evt.Data.(registry.Entry)
			if !ok {
				// Handle transaction events
				switch evt.Kind {
				case registry.TxBegin:
					if tx, ok := listener.(registry.TransactionListener); ok {
						tx.Begin(ctx)
					}
					return nil
				case registry.TxCommit:
					if tx, ok := listener.(registry.TransactionListener); ok {
						tx.Commit(ctx)
					}
					return nil
				case registry.TxDiscard:
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
			if evt.Kind != registry.EntryDelete && entry.Data == nil {
				reject(ctx, bus, entry.ID, NewConfigDataRequiredError())
				return nil
			}

			var err error
			switch evt.Kind {
			case registry.EntryCreate:
				err = listener.Add(ctx, entry)
			case registry.EntryUpdate:
				err = listener.Update(ctx, entry)
			case registry.EntryDelete:
				err = listener.Delete(ctx, entry)
			default:
				err = NewUnknownEventKindError(evt.Kind)
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

func accept(ctx context.Context, bus event.Bus, id registry.ID) {
	bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   registry.EntryAccept,
		Path:   id.String(),
	})
}

func reject(ctx context.Context, bus event.Bus, id registry.ID, err error) {
	bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   registry.EntryReject,
		Path:   id.String(),
		Data:   err,
	})
}

// NewTransactionHandler creates an event handler that only processes transaction events
func NewTransactionHandler(listener registry.TransactionListener) eventbus.EventHandler {
	return eventbus.NewBaseHandler(
		eventbus.Pattern{System: registry.System, Kind: registry.AllEvents},
		func(ctx context.Context, evt event.Event) error {
			switch evt.Kind {
			case registry.TxBegin:
				listener.Begin(ctx)
				return nil
			case registry.TxCommit:
				listener.Commit(ctx)
				return nil
			case registry.TxDiscard:
				listener.Discard(ctx)
				return nil
			default:
				return nil
			}
		},
	)
}
