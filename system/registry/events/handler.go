// SPDX-License-Identifier: MPL-2.0

package events

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/wildcard"
	"github.com/wippyai/runtime/system/eventbus"
)

// ErrSkipOperation is a special error type that indicates the operation should be skipped
// without triggering a reject event
var ErrSkipOperation = errors.New("skip operation")

var transactionParticipantSeq atomic.Uint64

// NewRegistryHandler adapts a registry listener with pattern matching to event router
func NewRegistryHandler(kinds registry.Kind, listener registry.EntryListener) eventbus.EventHandler {
	w := wildcard.NewWildcard(kinds)
	txParticipantID := ""
	if _, ok := listener.(registry.TransactionListener); ok {
		txParticipantID = nextTransactionParticipantID()
	}

	inner := eventbus.NewBaseHandler(
		eventbus.Pattern{System: registry.System, Kind: registry.AllEvents},
		func(ctx context.Context, evt event.Event) error {
			bus := event.GetBus(ctx)
			entry, ok := evt.Data.(registry.Entry)
			if !ok {
				// Handle transaction events
				switch evt.Kind {
				case registry.TxBegin:
					if tx, ok := listener.(registry.TransactionListener); ok {
						transactionReply(ctx, bus, evt.Path, txParticipantID, tx.Begin(ctx))
					}
					return nil
				case registry.TxCommit:
					if tx, ok := listener.(registry.TransactionListener); ok {
						transactionReply(ctx, bus, evt.Path, txParticipantID, tx.Commit(ctx))
					}
					return nil
				case registry.TxDiscard:
					if tx, ok := listener.(registry.TransactionListener); ok {
						transactionReply(ctx, bus, evt.Path, txParticipantID, tx.Discard(ctx))
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
	return &transactionAwareHandler{inner: inner, transactionParticipantID: txParticipantID}
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

func transactionReply(ctx context.Context, bus event.Bus, path event.Path, participantID string, err error) {
	if bus == nil || participantID == "" {
		return
	}
	kind := registry.TxAccept
	var data any
	if err != nil {
		kind = registry.TxReject
		data = err
	}
	bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   kind,
		Path:   participantReplyPath(path, participantID),
		Data:   data,
	})
}

// NewTransactionHandler creates an event handler that only processes transaction events
func NewTransactionHandler(listener registry.TransactionListener) eventbus.EventHandler {
	txParticipantID := nextTransactionParticipantID()
	inner := eventbus.NewBaseHandler(
		eventbus.Pattern{System: registry.System, Kind: registry.AllEvents},
		func(ctx context.Context, evt event.Event) error {
			bus := event.GetBus(ctx)
			switch evt.Kind {
			case registry.TxBegin:
				transactionReply(ctx, bus, evt.Path, txParticipantID, listener.Begin(ctx))
				return nil
			case registry.TxCommit:
				transactionReply(ctx, bus, evt.Path, txParticipantID, listener.Commit(ctx))
				return nil
			case registry.TxDiscard:
				transactionReply(ctx, bus, evt.Path, txParticipantID, listener.Discard(ctx))
				return nil
			default:
				return nil
			}
		},
	)
	return &transactionAwareHandler{inner: inner, transactionParticipantID: txParticipantID}
}

type transactionAwareHandler struct {
	inner                    eventbus.EventHandler
	transactionParticipantID string
}

func (h *transactionAwareHandler) Pattern() eventbus.Pattern {
	return h.inner.Pattern()
}

func (h *transactionAwareHandler) Handle(ctx context.Context, evt event.Event) error {
	return h.inner.Handle(ctx, evt)
}

func (h *transactionAwareHandler) RegistryTransactionParticipantID() string {
	return h.transactionParticipantID
}

func nextTransactionParticipantID() string {
	return "registry.tx." + strconv.FormatUint(transactionParticipantSeq.Add(1), 10)
}

func participantReplyPath(path event.Path, participantID string) event.Path {
	return path + "/" + participantID
}
