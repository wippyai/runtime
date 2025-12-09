package library

import (
	"context"

	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	"go.uber.org/zap"
)

type Manager struct {
	log  *zap.Logger
	code *lua.Manager
}

func NewManager(log *zap.Logger, code *lua.Manager) *Manager {
	return &Manager{log: log, code: code}
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindLibrary {
		return api.NewInvalidEntryKindError(entry.Kind, api.KindLibrary)
	}

	cfg, err := component.UnpackConfig[api.LibraryConfig](ctx, entry)
	if err != nil {
		return api.NewUnpackConfigError("library", err)
	}

	node := lua.Node{
		ID:     entry.ID,
		Kind:   api.KindLibrary,
		Source: cfg.Source,
	}

	if err := m.code.AddNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		return api.NewAddNodeError("library", err)
	}

	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindLibrary {
		return api.NewInvalidEntryKindError(entry.Kind, api.KindLibrary)
	}

	cfg, err := component.UnpackConfig[api.LibraryConfig](ctx, entry)
	if err != nil {
		return api.NewUnpackConfigError("library", err)
	}

	node := lua.Node{
		ID:     entry.ID,
		Kind:   api.KindLibrary,
		Source: cfg.Source,
	}

	if err := m.code.UpdateNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		return api.NewUpdateNodeError("library", err)
	}

	return nil
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindLibrary {
		return api.NewInvalidEntryKindError(entry.Kind, api.KindLibrary)
	}

	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return api.NewDeleteNodeError("library", err)
	}

	return nil
}

func (m *Manager) Invalidate(_ context.Context, _ []registry.ID) {
	// Libraries are stored in the code manager's node graph and are automatically
	// recompiled when needed. No additional invalidation handling required.
}
