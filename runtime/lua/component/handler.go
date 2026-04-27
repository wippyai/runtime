// SPDX-License-Identifier: MPL-2.0

package component

import (
	"context"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/internal/wildcard"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	lua "github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/system/eventbus"
	eventhandlers "github.com/wippyai/runtime/system/registry/events"
)

type EntityHandler interface {
	registry.EntryListener
	Invalidate(context.Context, []registry.ID) error
}

type Handler struct {
	entity EntityHandler
	inner  eventbus.EventHandler
	kinds  *wildcard.Wildcard
}

func NewHandler(kinds registry.Kind, entityHandler EntityHandler) *Handler {
	return &Handler{
		entity: entityHandler,
		inner:  eventhandlers.NewRegistryHandler(kinds, entityHandler),
		kinds:  wildcard.NewWildcard(kinds),
	}
}

func (h *Handler) Pattern() eventbus.Pattern {
	return eventbus.Pattern{
		System: "(registry|lua)",
		Kind:   "(entry|lua).(create|update|delete|reset_code)",
	}
}

func (h *Handler) Handle(ctx context.Context, evt event.Event) error {
	// Handle Lua events first
	if evt.System == luaapi.System {
		if evt.Kind == luaapi.InvalidateNodes {
			return h.handleInvalidate(ctx, evt)
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

func (h *Handler) handleInvalidate(ctx context.Context, evt event.Event) error {
	switch req := evt.Data.(type) {
	case luaapi.InvalidateNodesRequest:
		return h.invalidateRequest(ctx, req)
	case *luaapi.InvalidateNodesRequest:
		if req != nil {
			return h.invalidateRequest(ctx, *req)
		}
	case []registry.ID:
		return h.entity.Invalidate(ctx, req)
	}
	return nil
}

func (h *Handler) invalidateRequest(ctx context.Context, req luaapi.InvalidateNodesRequest) error {
	ids := make([]registry.ID, 0, len(req.Nodes))
	for _, node := range req.Nodes {
		if h.kinds.Match(node.Kind) {
			ids = append(ids, node.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	err := h.entity.Invalidate(ctx, ids)
	if req.AckPrefix == "" {
		return err
	}

	bus := event.GetBus(ctx)
	if bus == nil {
		return err
	}
	kind := luaapi.InvalidateNodesAccept
	var data any
	if err != nil {
		kind = luaapi.InvalidateNodesReject
		data = err
	}
	for _, id := range ids {
		bus.Send(ctx, event.Event{
			System: luaapi.System,
			Kind:   kind,
			Path:   req.AckPrefix + "/" + id.String(),
			Data:   data,
		})
	}
	return err
}

// UnpackConfig unpacks entry configuration (todo: see internal entry unpack)
func UnpackConfig[T any](ctx context.Context, entry registry.Entry) (*T, error) {
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		return nil, luaapi.ErrTranscoderNotFound
	}

	cfg := new(T)
	if err := dtt.Unmarshal(entry.Data, cfg); err != nil {
		return nil, runtimelua.NewUnmarshalConfigError(err)
	}

	if validator, ok := any(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, runtimelua.NewValidationError(err)
		}
	}

	return cfg, nil
}

// BuildImports creates imports from a list of IDs and their aliases
func BuildImports(imports map[string]registry.ID, modules []string) []lua.Import {
	out := make([]lua.Import, 0, len(imports))
	for k, v := range imports {
		out = append(out, lua.Import{ID: v, Alias: k})
	}

	for _, module := range modules {
		out = append(out, lua.Import{ID: registry.NewID("", module), Alias: module})
	}

	return out
}
