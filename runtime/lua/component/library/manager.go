// SPDX-License-Identifier: MPL-2.0

package library

import (
	"context"

	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	lua "github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	"go.uber.org/zap"
)

// Manager handles both source and bytecode Lua libraries.
type Manager struct {
	log        *zap.Logger
	code       *lua.Manager
	fsRegistry fsapi.Registry
}

// NewManager creates a new library manager.
// fsRegistry can be nil if bytecode libraries are not used.
func NewManager(log *zap.Logger, code *lua.Manager, fsRegistry fsapi.Registry) *Manager {
	return &Manager{
		log:        log,
		code:       code,
		fsRegistry: fsRegistry,
	}
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.Library:
		return m.addSource(ctx, entry)
	case api.LibraryBytecode:
		return m.addBytecode(ctx, entry)
	default:
		return runtimelua.NewInvalidEntryKindError(entry.Kind, api.Library)
	}
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.Library:
		return m.updateSource(ctx, entry)
	case api.LibraryBytecode:
		return m.updateBytecode(ctx, entry)
	default:
		return runtimelua.NewInvalidEntryKindError(entry.Kind, api.Library)
	}
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.Library, api.LibraryBytecode:
		if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
			return runtimelua.NewDeleteNodeError("library", err)
		}
		m.log.Debug("library deleted", zap.String("id", entry.ID.String()))
		return nil
	default:
		return runtimelua.NewInvalidEntryKindError(entry.Kind, api.Library)
	}
}

func (m *Manager) Invalidate(_ context.Context, _ []registry.ID) error {
	// Libraries are stored in the code manager's node graph and are automatically
	// recompiled when needed. No additional invalidation handling required.
	return nil
}

// addSource adds a source-based library.
func (m *Manager) addSource(ctx context.Context, entry registry.Entry) error {
	cfg, err := component.UnpackConfig[api.LibraryConfig](ctx, entry)
	if err != nil {
		return runtimelua.NewUnpackConfigError("library", err)
	}

	node := lua.Node{
		ID:     entry.ID,
		Kind:   api.Library,
		Source: cfg.Source,
	}

	if err := m.code.AddNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		return runtimelua.NewAddNodeError("library", err)
	}

	m.log.Debug("library added", zap.String("id", entry.ID.String()))
	return nil
}

// addBytecode adds a bytecode-based library.
func (m *Manager) addBytecode(ctx context.Context, entry registry.Entry) error {
	cfg, err := component.UnpackConfig[api.BytecodeLibraryConfig](ctx, entry)
	if err != nil {
		return runtimelua.NewUnpackConfigError("library", err)
	}

	proto, err := component.LoadAndVerifyBytecode(m.fsRegistry, cfg.FS, cfg.Path, cfg.Hash)
	if err != nil {
		return runtimelua.NewLoadBytecodeError(err)
	}

	node := lua.Node{
		ID:   entry.ID,
		Kind: api.LibraryBytecode,
	}

	if err := m.code.AddNodeWithProto(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules), proto); err != nil {
		return runtimelua.NewAddNodeError("library", err)
	}

	m.log.Debug("bytecode library added",
		zap.String("id", entry.ID.String()),
		zap.String("fs", cfg.FS),
		zap.String("path", cfg.Path),
	)
	return nil
}

// updateSource updates a source-based library.
func (m *Manager) updateSource(ctx context.Context, entry registry.Entry) error {
	cfg, err := component.UnpackConfig[api.LibraryConfig](ctx, entry)
	if err != nil {
		return runtimelua.NewUnpackConfigError("library", err)
	}

	node := lua.Node{
		ID:     entry.ID,
		Kind:   api.Library,
		Source: cfg.Source,
	}

	if err := m.code.UpdateNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		return runtimelua.NewUpdateNodeError("library", err)
	}

	m.log.Debug("library updated", zap.String("id", entry.ID.String()))
	return nil
}

// updateBytecode updates a bytecode-based library.
func (m *Manager) updateBytecode(ctx context.Context, entry registry.Entry) error {
	cfg, err := component.UnpackConfig[api.BytecodeLibraryConfig](ctx, entry)
	if err != nil {
		return runtimelua.NewUnpackConfigError("library", err)
	}

	proto, err := component.LoadAndVerifyBytecode(m.fsRegistry, cfg.FS, cfg.Path, cfg.Hash)
	if err != nil {
		return runtimelua.NewLoadBytecodeError(err)
	}

	node := lua.Node{
		ID:   entry.ID,
		Kind: api.LibraryBytecode,
	}

	if err := m.code.UpdateNodeWithProto(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules), proto); err != nil {
		return runtimelua.NewUpdateNodeError("library", err)
	}

	m.log.Debug("bytecode library updated", zap.String("id", entry.ID.String()))
	return nil
}

// Compile-time check
var _ registry.EntryListener = (*Manager)(nil)
