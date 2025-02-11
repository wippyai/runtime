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

type Factory interface {
	registry.EntryListener
	Invalidate([]registry.ID)
}

type Handler struct {
	factory Factory
	inner   eventbus.EventHandler
}

func NewHandler(kinds registry.Kind, factory Factory) *Handler {
	return &Handler{
		factory: factory,
		inner:   eventhandlers.NewRegistryHandler(kinds, factory),
	}
}

func (h *Handler) Pattern() eventbus.Pattern {
	return eventbus.Pattern{
		System: "(inner|lua)",
		Kind:   "(entry|lua).(create|update|delete|reset_code)",
	}
}

func (h *Handler) Handle(ctx context.Context, evt events.Event) error {
	// Handle Lua events first
	if evt.System == api.System {
		switch evt.Kind {
		case api.EventResetNodes:
			if ids, ok := evt.Data.([]registry.ID); ok {
				h.factory.Invalidate(ids)
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
func BuildImports(ids []registry.ID, importAliases map[string]registry.ID) []lua.Import {
	// inverse the import map
	aliases := make(map[registry.ID]string, len(importAliases))
	for k, v := range importAliases {
		aliases[v] = k
	}

	imports := make([]lua.Import, 0, len(ids))
	for _, id := range ids {
		imports = append(imports, lua.Import{
			ID:    id,
			Alias: aliases[id],
		})
	}

	return imports
}
