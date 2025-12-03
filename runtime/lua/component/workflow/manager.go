// Package workflow2 provides Lua workflow management for engine2.
// Workflows have restricted module access for deterministic execution.
package workflow

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Manager handles Lua workflow components for engine2.
// Workflows have restricted module access compared to processes.
type Manager struct {
	log     *zap.Logger
	code    *code.Manager
	bus     event.Bus
	configs sync.Map // map[registry.ID]*api.WorkflowConfig
}

// NewManager creates a new workflow manager.
func NewManager(log *zap.Logger, code *code.Manager, bus event.Bus) *Manager {
	return &Manager{
		log:  log,
		code: code,
		bus:  bus,
	}
}

// Add implements registry.EntryListener.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindWorkflow {
		return NewInvalidEntryKindError(string(entry.Kind), string(api.KindWorkflow))
	}

	cfg, err := component.UnpackConfig[api.WorkflowConfig](ctx, entry)
	if err != nil {
		return NewUnpackConfigError(err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindWorkflow,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.AddNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		return NewAddWorkflowNodeError(err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.registerFactory(ctx, entry.ID, cfg.Method); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		return NewRegisterFactoryError(err)
	}

	m.log.Debug("added workflow", zap.String("id", entry.ID.String()))
	return nil
}

// Update implements registry.EntryListener.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindWorkflow {
		return NewInvalidEntryKindError(string(entry.Kind), string(api.KindWorkflow))
	}

	cfg, err := component.UnpackConfig[api.WorkflowConfig](ctx, entry)
	if err != nil {
		return NewUnpackConfigError(err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindWorkflow,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.UpdateNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		return NewUpdateWorkflowNodeError(err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.registerFactory(ctx, entry.ID, cfg.Method); err != nil {
		return NewUpdateFactoryError(err)
	}

	m.log.Debug("updated workflow", zap.String("id", entry.ID.String()))
	return nil
}

// Delete implements registry.EntryListener.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindWorkflow {
		return NewInvalidEntryKindError(string(entry.Kind), string(api.KindWorkflow))
	}

	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return NewDeleteWorkflowNodeError(err)
	}

	m.configs.Delete(entry.ID)
	m.unregisterFactory(ctx, entry.ID)

	m.log.Debug("deleted workflow", zap.String("id", entry.ID.String()))
	return nil
}

// Invalidate handles code invalidation for hot reload.
func (m *Manager) Invalidate(ctx context.Context, ids []registry.ID) {
	for _, id := range ids {
		cfgAny, exists := m.configs.Load(id)
		if !exists {
			continue
		}
		cfg := cfgAny.(*api.WorkflowConfig)

		m.log.Debug("invalidating workflow", zap.String("id", id.String()))

		if err := m.registerFactory(ctx, id, cfg.Method); err != nil {
			m.log.Error("failed to invalidate workflow", zap.Error(err))
		}
	}
}

// registerFactory registers a workflow factory with the factory registry.
func (m *Manager) registerFactory(ctx context.Context, id registry.ID, method string) error {
	// Verify compilation works
	_, err := m.code.Compile(id, workflowBuildOptions())
	if err != nil {
		return NewCompileError(err)
	}

	// Default method
	if method == "" {
		method = "main"
	}

	m.bus.Send(ctx, event.Event{
		System: process.System,
		Kind:   process.FactoryRegister,
		Path:   id.String(),
		Data: &process.FactoryEntry{
			Factory: func() (process.Process, error) {
				return m.createProcess(id)
			},
			Meta: process.Meta{
				Method: method,
			},
		},
	})

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

// createProcess creates a new workflow process instance.
func (m *Manager) createProcess(id registry.ID) (process.Process, error) {
	compiled, err := m.code.Compile(id, workflowBuildOptions())
	if err != nil {
		return nil, NewCompileError(err)
	}

	return createProcess(compiled)
}

// createProcess creates a workflow process from compiled code.
// Uses restricted module set for deterministic execution.
func createProcess(compiled *code.CompiledMain) (process.Process, error) {
	// Workflows use restricted binders - no HTTP, no time yields
	binders := engine.CoreBinders()

	// Add module binders for dependencies
	for _, dep := range compiled.Dependencies {
		if dep.Node != nil && dep.Node.Module != nil {
			mod := dep.Node.Module
			name := dep.Name
			binders = append(binders, func(L *lua.LState) {
				L.PreloadModule(name, func(L *lua.LState) int {
					return mod.Loader(L)
				})
			})
		}
		if dep.Proto != nil {
			proto := dep.Proto
			name := dep.Name
			binders = append(binders, func(L *lua.LState) {
				L.PreloadModule(name, func(L *lua.LState) int {
					fn := L.LoadProto(proto)
					L.Push(fn)
					L.Call(0, 1)
					return 1
				})
			})
		}
	}

	// Add preloaded modules
	for _, pre := range compiled.Preloaded {
		if pre.Node != nil && pre.Node.Module != nil {
			mod := pre.Node.Module
			name := pre.Name
			binders = append(binders, func(L *lua.LState) {
				L.PreloadModule(name, func(L *lua.LState) int {
					return mod.Loader(L)
				})
			})
		}
		if pre.Proto != nil {
			proto := pre.Proto
			name := pre.Name
			binders = append(binders, func(L *lua.LState) {
				L.PreloadModule(name, func(L *lua.LState) int {
					fn := L.LoadProto(proto)
					L.Push(fn)
					L.Call(0, 1)
					return 1
				})
			})
		}
	}

	cfg := engine.FactoryConfig{
		Proto:         compiled.Main,
		ModuleBinders: binders,
	}

	factory := engine.NewFactory(cfg)
	return factory()
}

// workflowBuildOptions returns build options for workflows.
// Uses AllowListed mode to restrict available modules for determinism.
func workflowBuildOptions() *code.BuildOptions {
	return code.NewBuildOptions().
		WithMode(code.AllowListed).
		WithAllowed(
			registry.ID{Name: "json"},
			registry.ID{Name: "base64"},
			registry.ID{Name: "payload"},
			registry.ID{Name: "workflow"},
			registry.ID{Name: "channel"},
		)
}
