// Package process2 provides Lua process management for engine2.
package process2

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/modules/http"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	"github.com/wippyai/runtime/runtime/lua/modules/stream"
	timeyields "github.com/wippyai/runtime/runtime/lua/modules/time"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Manager handles Lua process components for engine2.
type Manager struct {
	log     *zap.Logger
	code    *code.Manager
	bus     event.Bus
	configs sync.Map // map[registry.ID]*api.ProcessConfig
}

// NewManager creates a new process manager.
func NewManager(log *zap.Logger, code *code.Manager, bus event.Bus) *Manager {
	return &Manager{
		log:  log.Named("process2"),
		code: code,
		bus:  bus,
	}
}

// Add implements registry.EntryListener.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindProcess {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindProcess)
	}

	cfg, err := component.UnpackConfig[api.ProcessConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack process config: %w", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindProcess,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.AddNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		return fmt.Errorf("failed to add process node: %w", err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.registerFactory(ctx, entry.ID, cfg.Method); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		return fmt.Errorf("failed to register factory: %w", err)
	}

	m.log.Debug("added process", zap.String("id", entry.ID.String()))
	return nil
}

// Update implements registry.EntryListener.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindProcess {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindProcess)
	}

	cfg, err := component.UnpackConfig[api.ProcessConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack process config: %w", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindProcess,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.UpdateNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		return fmt.Errorf("failed to update process node: %w", err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.registerFactory(ctx, entry.ID, cfg.Method); err != nil {
		return fmt.Errorf("failed to update factory: %w", err)
	}

	m.log.Debug("updated process", zap.String("id", entry.ID.String()))
	return nil
}

// Delete implements registry.EntryListener.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindProcess {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindProcess)
	}

	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return fmt.Errorf("failed to delete process node: %w", err)
	}

	m.configs.Delete(entry.ID)
	m.unregisterFactory(ctx, entry.ID)

	m.log.Debug("deleted process", zap.String("id", entry.ID.String()))
	return nil
}

// Invalidate handles code invalidation for hot reload.
func (m *Manager) Invalidate(ctx context.Context, ids []registry.ID) {
	for _, id := range ids {
		cfgAny, exists := m.configs.Load(id)
		if !exists {
			continue
		}
		cfg := cfgAny.(*api.ProcessConfig)

		m.log.Debug("invalidating process", zap.String("id", id.String()))

		if err := m.registerFactory(ctx, id, cfg.Method); err != nil {
			m.log.Error("failed to invalidate process", zap.Error(err))
		}
	}
}

// registerFactory registers a process factory with the factory registry.
func (m *Manager) registerFactory(ctx context.Context, id registry.ID, method string) error {
	// Verify compilation works
	_, err := m.code.Compile(id, processBuildOptions())
	if err != nil {
		return fmt.Errorf("failed to compile: %w", err)
	}

	// Default method
	if method == "" {
		method = "main"
	}

	m.bus.Send(ctx, event.Event{
		System: process2.FactorySystem,
		Kind:   process2.FactoryRegister,
		Path:   id.String(),
		Data: &process2.FactoryEntry{
			Factory: func() (process2.Process, error) {
				return m.createProcess(id)
			},
			Meta: process2.ProcessMeta{
				Method: method,
			},
		},
	})

	return nil
}

// unregisterFactory removes a factory registration.
func (m *Manager) unregisterFactory(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: process2.FactorySystem,
		Kind:   process2.FactoryDelete,
		Path:   id.String(),
	})
}

// createProcess creates a new process instance.
func (m *Manager) createProcess(id registry.ID) (process2.Process, error) {
	compiled, err := m.code.Compile(id, processBuildOptions())
	if err != nil {
		return nil, fmt.Errorf("failed to compile: %w", err)
	}

	return createProcess(compiled)
}

// createProcess creates a process from compiled code.
func createProcess(compiled *code.CompiledMain) (process2.Process, error) {
	binders := engine.CoreBinders()
	binders = append(binders, stream.BindStream, http.Bind, timeyields.BindYields, processmod.BindGlobal)

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
					L.Call(1, 1)
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
					L.Call(1, 1)
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

// processBuildOptions returns build options for processes.
func processBuildOptions() *code.BuildOptions {
	return code.NewBuildOptions().
		WithMode(code.AllowAll)
}
