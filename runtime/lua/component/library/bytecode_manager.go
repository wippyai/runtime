// Package library provides Lua library management.
package library

import (
	"context"

	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	"go.uber.org/zap"
)

// BytecodeManager handles precompiled Lua bytecode library loading.
type BytecodeManager struct {
	log        *zap.Logger
	code       *lua.Manager
	fsRegistry fsapi.Registry
}

// NewBytecodeManager creates a new bytecode library manager.
func NewBytecodeManager(log *zap.Logger, code *lua.Manager, fsReg fsapi.Registry) *BytecodeManager {
	return &BytecodeManager{
		log:        log,
		code:       code,
		fsRegistry: fsReg,
	}
}

// Add loads and registers a new bytecode library.
func (m *BytecodeManager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindLibraryBytecode {
		return api.NewInvalidEntryKindError(entry.Kind, api.KindLibraryBytecode)
	}

	cfg, err := component.UnpackConfig[api.BytecodeLibraryConfig](ctx, entry)
	if err != nil {
		return api.NewUnpackConfigError("library", err)
	}

	proto, err := component.LoadAndVerifyBytecode(m.fsRegistry, cfg.FS, cfg.Path, cfg.Hash)
	if err != nil {
		return api.NewLoadBytecodeError(err)
	}

	node := lua.Node{
		ID:   entry.ID,
		Kind: api.KindLibraryBytecode,
	}

	if err := m.code.AddNodeWithProto(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules), proto); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		return api.NewAddNodeError("library", err)
	}

	m.log.Info("bytecode library added",
		zap.String("id", entry.ID.String()),
		zap.String("fs", cfg.FS),
		zap.String("path", cfg.Path),
	)

	return nil
}

// Update updates an existing bytecode library.
func (m *BytecodeManager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindLibraryBytecode {
		return api.NewInvalidEntryKindError(string(entry.Kind), string(api.KindLibraryBytecode))
	}

	cfg, err := component.UnpackConfig[api.BytecodeLibraryConfig](ctx, entry)
	if err != nil {
		return api.NewUnpackConfigError("library", err)
	}

	proto, err := component.LoadAndVerifyBytecode(m.fsRegistry, cfg.FS, cfg.Path, cfg.Hash)
	if err != nil {
		return api.NewLoadBytecodeError(err)
	}

	node := lua.Node{
		ID:   entry.ID,
		Kind: api.KindLibraryBytecode,
	}

	if err := m.code.UpdateNodeWithProto(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules), proto); err != nil {
		return api.NewUpdateNodeError("library", err)
	}

	m.log.Info("bytecode library updated", zap.String("id", entry.ID.String()))
	return nil
}

// Delete removes a bytecode library.
func (m *BytecodeManager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindLibraryBytecode {
		return api.NewInvalidEntryKindError(string(entry.Kind), string(api.KindLibraryBytecode))
	}

	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return api.NewDeleteNodeError("library", err)
	}

	m.log.Info("bytecode library deleted", zap.String("id", entry.ID.String()))
	return nil
}

// Invalidate handles code invalidation for hot reload.
func (m *BytecodeManager) Invalidate(_ context.Context, _ []registry.ID) {
	// Libraries are stored in the code manager's node graph and are automatically
	// recompiled when needed. No additional invalidation handling required.
}

// Compile-time check
var _ registry.EntryListener = (*BytecodeManager)(nil)
