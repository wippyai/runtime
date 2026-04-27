// SPDX-License-Identifier: MPL-2.0

package boot

import (
	"context"
	"strconv"
	"sync/atomic"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/wildcard"
	"github.com/wippyai/runtime/system/eventbus"
)

// HandlerRegistry collects event handlers during plugin initialization.
type HandlerRegistry interface {
	// Register registers an event handler for a specific pattern
	Register(handler eventbus.EventHandler)

	// RegisterListener registers a registry.EntryListener for matching registry events.
	// This sends Accept/Reject events - use for primary handlers only.
	RegisterListener(kinds registry.Kind, listener registry.EntryListener)

	// RegisterObserver registers a registry.EntryListener that only observes events.
	// Does not send Accept/Reject - use for secondary handlers that shouldn't ack.
	RegisterObserver(kinds registry.Kind, listener registry.EntryListener)

	// Handlers returns all registered handlers
	Handlers() []eventbus.EventHandler

	// TransactionParticipants returns handler reply ids for registry transaction barriers.
	TransactionParticipants() []string
}

type handlerRegistry struct {
	handlers []eventbus.EventHandler
}

var bootTransactionParticipantSeq atomic.Uint64

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

func (r *handlerRegistry) TransactionParticipants() []string {
	participants := make([]string, 0, len(r.handlers))
	for _, handler := range r.handlers {
		participant, ok := handler.(registry.TransactionParticipant)
		if !ok {
			continue
		}
		id := participant.RegistryTransactionParticipantID()
		if id != "" {
			participants = append(participants, id)
		}
	}
	return participants
}

func (r *handlerRegistry) RegisterObserver(kinds registry.Kind, listener registry.EntryListener) {
	handler := wrapObserver(kinds, listener)
	r.handlers = append(r.handlers, handler)
}

// handleTransaction dispatches transaction events to the listener if it implements TransactionListener.
func handleTransaction(ctx context.Context, kind event.Kind, listener registry.EntryListener) (bool, error) {
	tx, ok := listener.(registry.TransactionListener)
	if !ok {
		return false, nil
	}
	switch kind {
	case registry.TxBegin:
		return true, tx.Begin(ctx)
	case registry.TxCommit:
		return true, tx.Commit(ctx)
	case registry.TxDiscard:
		return true, tx.Discard(ctx)
	default:
		return false, nil
	}
}

// dispatchEntry calls the appropriate listener method based on event kind.
func dispatchEntry(ctx context.Context, kind event.Kind, entry registry.Entry, listener registry.EntryListener) error {
	switch kind {
	case registry.EntryCreate:
		return listener.Add(ctx, entry)
	case registry.EntryUpdate:
		return listener.Update(ctx, entry)
	case registry.EntryDelete:
		return listener.Delete(ctx, entry)
	}
	return nil
}

// wrapListener wraps a registry.EntryListener into an eventbus.EventHandler.
// Sends Accept/Reject events for primary handlers.
func wrapListener(kinds registry.Kind, listener registry.EntryListener) eventbus.EventHandler {
	w := wildcard.NewWildcard(kinds)
	txParticipantID := ""
	if _, ok := listener.(registry.TransactionListener); ok {
		txParticipantID = nextBootTransactionParticipantID()
	}

	inner := eventbus.NewBaseHandler(
		eventbus.Pattern{System: registry.System, Kind: registry.AllEvents},
		func(ctx context.Context, evt event.Event) error {
			entry, ok := evt.Data.(registry.Entry)
			if !ok {
				if handled, err := handleTransaction(ctx, evt.Kind, listener); handled {
					transactionReply(ctx, evt.Path, txParticipantID, err)
				}
				return nil
			}

			if !w.Match(entry.Kind) {
				return nil
			}

			bus := event.GetBus(ctx)
			if err := dispatchEntry(ctx, evt.Kind, entry, listener); err != nil {
				bus.Send(ctx, event.Event{
					System: registry.System,
					Kind:   registry.EntryReject,
					Path:   entry.ID.String(),
					Data:   err,
				})
				return nil
			}

			bus.Send(ctx, event.Event{
				System: registry.System,
				Kind:   registry.EntryAccept,
				Path:   entry.ID.String(),
			})
			return nil
		},
	)
	return &transactionAwareHandler{inner: inner, transactionParticipantID: txParticipantID}
}

// wrapObserver wraps a registry.EntryListener for observation only.
// Does not send Accept/Reject - for secondary handlers that observe but don't ack.
func wrapObserver(kinds registry.Kind, listener registry.EntryListener) eventbus.EventHandler {
	w := wildcard.NewWildcard(kinds)
	txParticipantID := ""
	if _, ok := listener.(registry.TransactionListener); ok {
		txParticipantID = nextBootTransactionParticipantID()
	}

	inner := eventbus.NewBaseHandler(
		eventbus.Pattern{System: registry.System, Kind: registry.AllEvents},
		func(ctx context.Context, evt event.Event) error {
			entry, ok := evt.Data.(registry.Entry)
			if !ok {
				if handled, err := handleTransaction(ctx, evt.Kind, listener); handled {
					transactionReply(ctx, evt.Path, txParticipantID, err)
				}
				return nil
			}

			if !w.Match(entry.Kind) {
				return nil
			}

			_ = dispatchEntry(ctx, evt.Kind, entry, listener)
			return nil
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

func transactionReply(ctx context.Context, path event.Path, participantID string, err error) {
	bus := event.GetBus(ctx)
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

func nextBootTransactionParticipantID() string {
	return "boot.tx." + strconv.FormatUint(bootTransactionParticipantSeq.Add(1), 10)
}

func participantReplyPath(path event.Path, participantID string) event.Path {
	return path + "/" + participantID
}

var handlerRegistryKey = &ctxapi.Key{Name: "boot.handler_registry"}

// WithHandlerRegistry stores the handler registry in AppContext.
func WithHandlerRegistry(ctx context.Context, registry HandlerRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(handlerRegistryKey) == nil {
		ac.With(handlerRegistryKey, registry)
	}
	return ctx
}

// GetHandlerRegistry retrieves the handler registry from AppContext.
func GetHandlerRegistry(ctx context.Context) HandlerRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(handlerRegistryKey); val != nil {
		if reg, ok := val.(HandlerRegistry); ok {
			return reg
		}
	}
	return nil
}
