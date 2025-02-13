package factory

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	lua "github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/system/eventbus"
	eventhandlers "github.com/ponyruntime/pony/system/registry/events"
)

type EntityHandler interface {
	registry.EntryListener
	Invalidate([]registry.ID)
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

func (h *Handler) Handle(ctx context.Context, evt events.Event) error {
	// Handle Lua events first
	if evt.System == api.System {
		switch evt.Kind {
		case api.InvalidateNodes:
			if ids, ok := evt.Data.([]registry.ID); ok {
				h.entity.Invalidate(ids)
			}
			return nil
		}
	}

	return h.inner.Handle(ctx, evt)
}

func UnpackConfig[T any](ctx context.Context, entry registry.Entry) (*T, error) {
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		return nil, fmt.Errorf("transcoder not found in context")
	}

	cfg := new(T)
	if err := dtt.Unmarshal(entry.Data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if validator, ok := interface{}(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("invalid configuration: %w", err)
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
		out = append(out, lua.Import{ID: registry.ID{Name: module}, Alias: module})
	}

	return out
}
