package library

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	lua "github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/runtime/lua/component"
	"go.uber.org/zap"
)

type Manager struct {
	log  *zap.Logger
	code *lua.Manager
}

func NewManager(log *zap.Logger, code *lua.Manager) *Manager {
	return &Manager{log: log, code: code}
}

func NewLibraryManager(log *zap.Logger, code *lua.Manager) *component.Handler {
	return component.NewHandler(api.KindLibrary, NewManager(log, code))
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindLibrary {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindLibrary)
	}

	cfg, err := component.UnpackConfig[api.LibraryConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack library config: %w", err)
	}

	node := lua.Node{
		ID:     entry.ID,
		Kind:   api.KindLibrary,
		Source: cfg.Source,
	}

	if err := m.code.AddNode(ctx, node, component.BuildImports(cfg.Import, cfg.Modules)); err != nil {
		return fmt.Errorf("failed to add library node: %w", err)
	}

	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindLibrary {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindLibrary)
	}

	cfg, err := component.UnpackConfig[api.LibraryConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack library config: %w", err)
	}

	node := lua.Node{
		ID:     entry.ID,
		Kind:   api.KindLibrary,
		Source: cfg.Source,
	}

	if err := m.code.UpdateNode(ctx, node, component.BuildImports(cfg.Import, cfg.Modules)); err != nil {
		return fmt.Errorf("failed to update library node: %w", err)
	}

	return nil
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindLibrary {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindLibrary)
	}

	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return fmt.Errorf("failed to delete library node: %w", err)
	}

	return nil
}

func (m *Manager) Invalidate(ctx context.Context, ids []registry.ID) {
	for _, id := range ids {
		m.log.Debug("invalidating library", zap.String("id", id.String()))
	}
}
