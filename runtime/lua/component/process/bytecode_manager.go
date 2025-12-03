// Package process provides Lua process management.
package process

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	"go.uber.org/zap"
)

// BytecodeManager handles precompiled Lua bytecode process loading.
type BytecodeManager struct {
	log        *zap.Logger
	code       *code.Manager
	bus        event.Bus
	fsRegistry fsapi.Registry
	configs    sync.Map
}

// NewBytecodeManager creates a new bytecode process manager.
func NewBytecodeManager(log *zap.Logger, code *code.Manager, bus event.Bus, fsReg fsapi.Registry) *BytecodeManager {
	return &BytecodeManager{
		log:        log,
		code:       code,
		bus:        bus,
		fsRegistry: fsReg,
	}
}

// Add loads and registers a new bytecode process.
func (m *BytecodeManager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindProcessBytecode {
		return NewInvalidEntryKindError(string(entry.Kind), string(api.KindProcessBytecode))
	}

	cfg, err := component.UnpackConfig[api.BytecodeProcessConfig](ctx, entry)
	if err != nil {
		return NewUnpackConfigError(err)
	}

	proto, err := component.LoadAndVerifyBytecode(m.fsRegistry, cfg.FS, cfg.Path, cfg.Hash)
	if err != nil {
		return NewLoadBytecodeError(err)
	}

	// Add node with proto to code manager
	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindProcessBytecode,
		Method: cfg.Method,
	}

	if err := m.code.AddNodeWithProto(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules), proto); err != nil {
		return NewAddProcessNodeError(err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.registerFactory(ctx, entry.ID, cfg.Method); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		return NewRegisterFactoryError(err)
	}

	m.log.Info("bytecode process added",
		zap.String("id", entry.ID.String()),
		zap.String("fs", cfg.FS),
		zap.String("path", cfg.Path),
	)

	return nil
}

// Update updates an existing bytecode process.
func (m *BytecodeManager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindProcessBytecode {
		return NewInvalidEntryKindError(string(entry.Kind), string(api.KindProcessBytecode))
	}

	cfg, err := component.UnpackConfig[api.BytecodeProcessConfig](ctx, entry)
	if err != nil {
		return NewUnpackConfigError(err)
	}

	proto, err := component.LoadAndVerifyBytecode(m.fsRegistry, cfg.FS, cfg.Path, cfg.Hash)
	if err != nil {
		return NewLoadBytecodeError(err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindProcessBytecode,
		Method: cfg.Method,
	}

	if err := m.code.UpdateNodeWithProto(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules), proto); err != nil {
		return NewUpdateProcessNodeError(err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.registerFactory(ctx, entry.ID, cfg.Method); err != nil {
		return NewUpdateFactoryError(err)
	}

	m.log.Info("bytecode process updated", zap.String("id", entry.ID.String()))
	return nil
}

// Delete removes a bytecode process.
func (m *BytecodeManager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindProcessBytecode {
		return NewInvalidEntryKindError(string(entry.Kind), string(api.KindProcessBytecode))
	}

	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return NewDeleteProcessNodeError(err)
	}

	m.configs.Delete(entry.ID)
	m.unregisterFactory(ctx, entry.ID)

	m.log.Info("bytecode process deleted", zap.String("id", entry.ID.String()))
	return nil
}

// Invalidate handles code invalidation for hot reload.
func (m *BytecodeManager) Invalidate(ctx context.Context, ids []registry.ID) {
	for _, id := range ids {
		cfgAny, exists := m.configs.Load(id)
		if !exists {
			continue
		}
		cfg := cfgAny.(*api.BytecodeProcessConfig)

		m.log.Debug("invalidating bytecode process", zap.String("id", id.String()))

		if _, err := component.LoadAndVerifyBytecode(m.fsRegistry, cfg.FS, cfg.Path, cfg.Hash); err != nil {
			m.log.Error("failed to reload bytecode", zap.Error(err))
			continue
		}

		if err := m.registerFactory(ctx, id, cfg.Method); err != nil {
			m.log.Error("failed to invalidate bytecode process", zap.Error(err))
		}
	}
}

// registerFactory registers a process factory with the factory registry.
func (m *BytecodeManager) registerFactory(ctx context.Context, id registry.ID, method string) error {
	// Verify compilation works
	_, err := m.code.Compile(id, processBuildOptions())
	if err != nil {
		return NewCompileError(err)
	}

	if method == "" {
		method = "main"
	}

	m.bus.Send(ctx, event.Event{
		System: process.FactorySystem,
		Kind:   process.FactoryRegister,
		Path:   id.String(),
		Data: &process.FactoryEntry{
			Factory: func() (process.Process, error) {
				return m.createProcess(id)
			},
			Meta: process.ProcessMeta{
				Method: method,
			},
		},
	})

	return nil
}

// unregisterFactory removes a factory registration.
func (m *BytecodeManager) unregisterFactory(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: process.FactorySystem,
		Kind:   process.FactoryDelete,
		Path:   id.String(),
	})
}

// createProcess creates a new process instance.
func (m *BytecodeManager) createProcess(id registry.ID) (process.Process, error) {
	compiled, err := m.code.Compile(id, processBuildOptions())
	if err != nil {
		return nil, NewCompileError(err)
	}

	return createProcess(compiled)
}

// Compile-time check
var _ registry.EntryListener = (*BytecodeManager)(nil)
