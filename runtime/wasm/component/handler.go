// SPDX-License-Identifier: MPL-2.0

package component

import (
	"context"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	"github.com/wippyai/runtime/system/eventbus"
	eventhandlers "github.com/wippyai/runtime/system/registry/events"
)

// EntityHandler defines runtime handlers that react to registry and wasm invalidation events.
type EntityHandler interface {
	registry.EntryListener
	Invalidate(context.Context, []registry.ID)
}

// Handler bridges registry/wasm events to an entity handler.
type Handler struct {
	entity EntityHandler
	inner  eventbus.EventHandler
}

// NewHandler creates a handler for the given kinds and entity handler.
func NewHandler(kinds registry.Kind, entityHandler EntityHandler) *Handler {
	return &Handler{
		entity: entityHandler,
		inner:  eventhandlers.NewRegistryHandler(kinds, entityHandler),
	}
}

func (h *Handler) Pattern() eventbus.Pattern {
	return eventbus.Pattern{
		System: "(registry|wasm)",
		Kind:   "(entry|wasm).(create|update|delete|reset_code)",
	}
}

func (h *Handler) Handle(ctx context.Context, evt event.Event) error {
	if evt.System == wasmapi.System {
		if evt.Kind == wasmapi.InvalidateNodes {
			if ids, ok := evt.Data.([]registry.ID); ok {
				h.entity.Invalidate(ctx, ids)
			}
			return nil
		}
	}
	return h.inner.Handle(ctx, evt)
}

func (h *Handler) RegistryTransactionParticipantID() string {
	participant, ok := h.inner.(registry.TransactionParticipant)
	if !ok {
		return ""
	}
	return participant.RegistryTransactionParticipantID()
}

// UnpackConfig unpacks and validates entry configuration.
func UnpackConfig[T any](ctx context.Context, entry registry.Entry) (*T, error) {
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		return nil, runtimewasm.ErrTranscoderNotFound
	}

	cfg := new(T)
	if err := dtt.Unmarshal(entry.Data, cfg); err != nil {
		return nil, runtimewasm.NewUnpackConfigError("wasm", err)
	}

	if validator, ok := any(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, runtimewasm.NewValidationError(err)
		}
	}

	return cfg, nil
}
