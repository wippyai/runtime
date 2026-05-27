// SPDX-License-Identifier: MPL-2.0

// Package workflow provides Lua workflow management.
// Workflows have restricted module access for deterministic execution.
package workflow

import (
	"context"
	"errors"
	"sync"

	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	"github.com/wippyai/runtime/runtime/lua/engine"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	"go.uber.org/zap"
)

// configEntry holds config for either source or bytecode workflow.
type configEntry struct {
	source   *api.WorkflowConfig
	bytecode *api.BytecodeWorkflowConfig
	method   string
}

// Manager handles Lua workflow components for engine2.
// Workflows have restricted module access compared to processes.
type Manager struct {
	log     *zap.Logger
	code    *code.Manager
	bus     event.Bus
	fsReg   fsapi.Registry
	factory engine.CompiledFactory
	configs sync.Map // map[registry.ID]*configEntry
}

// NewManager creates a new workflow manager.
func NewManager(
	log *zap.Logger,
	code *code.Manager,
	bus event.Bus,
	fsReg fsapi.Registry,
	factory engine.CompiledFactory,
) *Manager {
	return &Manager{
		log:     log,
		code:    code,
		bus:     bus,
		fsReg:   fsReg,
		factory: factory,
	}
}

// Add implements registry.EntryListener.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.Workflow:
		return m.addSource(ctx, entry)
	case api.WorkflowBytecode:
		return m.addBytecode(ctx, entry)
	default:
		return runtimelua.NewInvalidEntryKindError(entry.Kind, api.Workflow)
	}
}

// Update implements registry.EntryListener.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.Workflow:
		return m.updateSource(ctx, entry)
	case api.WorkflowBytecode:
		return m.updateBytecode(ctx, entry)
	default:
		return runtimelua.NewInvalidEntryKindError(entry.Kind, api.Workflow)
	}
}

// Delete implements registry.EntryListener.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.Workflow, api.WorkflowBytecode:
		if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
			return runtimelua.NewDeleteNodeError("workflow", err)
		}
		m.configs.Delete(entry.ID)
		m.unregisterFactory(ctx, entry.ID)
		m.log.Debug("deleted workflow", zap.String("id", entry.ID.String()))
		return nil
	default:
		return runtimelua.NewInvalidEntryKindError(entry.Kind, api.Workflow)
	}
}

// Invalidate handles code invalidation for hot reload.
func (m *Manager) Invalidate(ctx context.Context, ids []registry.ID) error {
	var errs []error
	for _, id := range ids {
		cfgAny, exists := m.configs.Load(id)
		if !exists {
			continue
		}
		cfg := cfgAny.(*configEntry)

		m.log.Debug("invalidating workflow", zap.String("id", id.String()))

		// For bytecode, verify the file is still valid.
		if cfg.bytecode != nil {
			if _, err := component.LoadAndVerifyBytecode(m.fsReg, cfg.bytecode.FS, cfg.bytecode.Path, cfg.bytecode.Hash); err != nil {
				m.log.Error("failed to reload bytecode workflow", zap.Error(err))
				errs = append(errs, err)
				continue
			}
		}

		if err := m.registerFactory(ctx, id, cfg.method); err != nil {
			m.log.Error("failed to invalidate workflow", zap.Error(err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// addSource adds a source-based workflow.
func (m *Manager) addSource(ctx context.Context, entry registry.Entry) error {
	cfg, err := component.UnpackConfig[api.WorkflowConfig](ctx, entry)
	if err != nil {
		return runtimelua.NewUnpackConfigError("workflow", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.Workflow,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.AddNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		return runtimelua.NewAddNodeError("workflow", err)
	}

	m.configs.Store(entry.ID, &configEntry{
		source: cfg,
		method: cfg.Method,
	})

	if err := m.registerFactory(ctx, entry.ID, cfg.Method); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		m.configs.Delete(entry.ID)
		return runtimelua.NewRegisterFactoryError(err)
	}

	m.log.Debug("added workflow", zap.String("id", entry.ID.String()))
	return nil
}

// addBytecode adds a bytecode-based workflow.
func (m *Manager) addBytecode(ctx context.Context, entry registry.Entry) error {
	cfg, err := component.UnpackConfig[api.BytecodeWorkflowConfig](ctx, entry)
	if err != nil {
		return runtimelua.NewUnpackConfigError("workflow", err)
	}

	proto, err := component.LoadAndVerifyBytecode(m.fsReg, cfg.FS, cfg.Path, cfg.Hash)
	if err != nil {
		return runtimelua.NewLoadBytecodeError(err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.WorkflowBytecode,
		Method: cfg.Method,
	}

	if err := m.code.AddNodeWithProto(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules), proto); err != nil {
		return runtimelua.NewAddNodeError("workflow", err)
	}

	m.configs.Store(entry.ID, &configEntry{
		bytecode: cfg,
		method:   cfg.Method,
	})

	if err := m.registerFactory(ctx, entry.ID, cfg.Method); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		m.configs.Delete(entry.ID)
		return runtimelua.NewRegisterFactoryError(err)
	}

	m.log.Debug("added bytecode workflow",
		zap.String("id", entry.ID.String()),
		zap.String("fs", cfg.FS),
		zap.String("path", cfg.Path),
	)
	return nil
}

// updateSource updates a source-based workflow.
func (m *Manager) updateSource(ctx context.Context, entry registry.Entry) error {
	cfg, err := component.UnpackConfig[api.WorkflowConfig](ctx, entry)
	if err != nil {
		return runtimelua.NewUnpackConfigError("workflow", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.Workflow,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.UpdateNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		return runtimelua.NewUpdateNodeError("workflow", err)
	}

	m.configs.Store(entry.ID, &configEntry{
		source: cfg,
		method: cfg.Method,
	})

	if err := m.registerFactory(ctx, entry.ID, cfg.Method); err != nil {
		return runtimelua.NewUpdateFactoryError(err)
	}

	m.log.Debug("updated workflow", zap.String("id", entry.ID.String()))
	return nil
}

// updateBytecode updates a bytecode-based workflow.
func (m *Manager) updateBytecode(ctx context.Context, entry registry.Entry) error {
	cfg, err := component.UnpackConfig[api.BytecodeWorkflowConfig](ctx, entry)
	if err != nil {
		return runtimelua.NewUnpackConfigError("workflow", err)
	}

	proto, err := component.LoadAndVerifyBytecode(m.fsReg, cfg.FS, cfg.Path, cfg.Hash)
	if err != nil {
		return runtimelua.NewLoadBytecodeError(err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.WorkflowBytecode,
		Method: cfg.Method,
	}

	if err := m.code.UpdateNodeWithProto(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules), proto); err != nil {
		return runtimelua.NewUpdateNodeError("workflow", err)
	}

	m.configs.Store(entry.ID, &configEntry{
		bytecode: cfg,
		method:   cfg.Method,
	})

	if err := m.registerFactory(ctx, entry.ID, cfg.Method); err != nil {
		return runtimelua.NewUpdateFactoryError(err)
	}

	m.log.Debug("updated bytecode workflow", zap.String("id", entry.ID.String()))
	return nil
}

// registerFactory registers a workflow factory with the factory registry.
func (m *Manager) registerFactory(ctx context.Context, id registry.ID, method string) error {
	// Create factory with class-based filtering for deterministic execution.
	// Modules must have ClassDeterministic or ClassWorkflow to be allowed.
	factoryFn, err := m.factory.CreateFactory(id,
		engine.WithAllowedClasses(api.ClassDeterministic, api.ClassWorkflow),
		engine.WithModule(processmod.Module),
	)
	if err != nil {
		return err // Already has compile context from code.Manager
	}

	if method == "" {
		method = "main"
	}

	path := id.String()

	awaitSvc := event.GetAwaitService(ctx)
	if awaitSvc == nil {
		return runtimelua.NewRegisterFactoryError(nil)
	}

	waiter, err := awaitSvc.Prepare(ctx, process.System, "factory.(accept|reject)", path, event.DefaultAwaitTimeout)
	if err != nil {
		return runtimelua.NewRegisterFactoryError(err)
	}
	defer waiter.Close()

	m.bus.Send(ctx, event.Event{
		System: process.System,
		Kind:   process.FactoryRegister,
		Path:   path,
		Data: &process.FactoryEntry{
			Factory: factoryFn,
			Meta: process.Meta{
				Method: method,
			},
		},
	})

	result := waiter.Wait()
	if !result.Accepted {
		return runtimelua.NewRegisterFactoryError(result.Error)
	}

	return nil
}

// unregisterFactory removes a factory registration.
func (m *Manager) unregisterFactory(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: process.System,
		Kind:   process.FactoryDelete,
		Path:   id.String(),
	})
}

// Compile-time check
var _ registry.EntryListener = (*Manager)(nil)
