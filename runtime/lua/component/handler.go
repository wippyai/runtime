package component

import (
	"context"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/system/eventbus"
	eventhandlers "github.com/wippyai/runtime/system/registry/events"
)

type EntityHandler interface {
	registry.EntryListener
	Invalidate(context.Context, []registry.ID)
}

type Handler struct {
	entity EntityHandler
	inner  eventbus.EventHandler
}

func NewHandler(kinds registry.Kind, entityHandler EntityHandler) *Handler {
	return &Handler{
		entity: entityHandler,
		inner:  eventhandlers.NewRegistryHandler(kinds, entityHandler),
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
	if evt.System == api.System {
		if evt.Kind == api.InvalidateNodes {
			if ids, ok := evt.Data.([]registry.ID); ok {
				h.entity.Invalidate(ctx, ids)
			}
			return nil
		}
	}

	return h.inner.Handle(ctx, evt)
}

// todo: see internal entry unpack
func UnpackConfig[T any](ctx context.Context, entry registry.Entry) (*T, error) {
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		return nil, ErrTranscoderNotFound
	}

	cfg := new(T)
	if err := dtt.Unmarshal(entry.Data, cfg); err != nil {
		return nil, NewUnmarshalError(err)
	}

	if validator, ok := interface{}(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, NewValidationError(err)
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
