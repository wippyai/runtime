// Package process2 provides Lua process management for engine2.
package process

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
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	"github.com/wippyai/runtime/system/eventbus"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Manager handles Lua process components for engine2.
type Manager struct {
	log     *zap.Logger
	code    *code.Manager
	bus     event.Bus
	awaiter *eventbus.Awaiter
	configs sync.Map // map[registry.ID]*api.ProcessConfig
}

// NewManager creates a new process manager.
func NewManager(log *zap.Logger, code *code.Manager, bus event.Bus) *Manager {
	return &Manager{
		log:     log,
		code:    code,
		bus:     bus,
		awaiter: eventbus.NewAwaiter(bus, process.System, "factory.(accept|reject)"),
	}
}

// Add implements registry.EntryListener.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindProcess {
		return api.NewInvalidEntryKindError(entry.Kind, api.KindProcess)
	}

	cfg, err := component.UnpackConfig[api.ProcessConfig](ctx, entry)
	if err != nil {
		return api.NewUnpackConfigError("process", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindProcess,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.AddNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		return api.NewAddNodeError("process", err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.registerFactory(ctx, entry.ID, cfg.Method); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		return api.NewRegisterFactoryError(err)
	}

	m.log.Debug("added process", zap.String("id", entry.ID.String()))
	return nil
}

// Update implements registry.EntryListener.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindProcess {
		return api.NewInvalidEntryKindError(entry.Kind, api.KindProcess)
	}

	cfg, err := component.UnpackConfig[api.ProcessConfig](ctx, entry)
	if err != nil {
		return api.NewUnpackConfigError("process", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindProcess,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.UpdateNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		return api.NewUpdateNodeError("process", err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.registerFactory(ctx, entry.ID, cfg.Method); err != nil {
		return api.NewUpdateFactoryError(err)
	}

	m.log.Debug("updated process", zap.String("id", entry.ID.String()))
	return nil
}

// Delete implements registry.EntryListener.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindProcess {
		return api.NewInvalidEntryKindError(entry.Kind, api.KindProcess)
	}

	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return api.NewDeleteNodeError("process", err)
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

// registerFactory registers a process factory with the factory registry and waits for confirmation.
func (m *Manager) registerFactory(ctx context.Context, id registry.ID, method string) error {
	// Verify compilation works
	_, err := m.code.Compile(id, processBuildOptions())
	if err != nil {
		return api.NewCompileError(err)
	}

	// Default method
	if method == "" {
		method = "main"
	}

	path := id.String()

	// Subscribe BEFORE sending to avoid race condition
	waiter, err := m.awaiter.Prepare(ctx, path)
	if err != nil {
		return api.NewRegisterFactoryError(err)
	}

	m.bus.Send(ctx, event.Event{
		System: process.System,
		Kind:   process.FactoryRegister,
		Path:   path,
		Data: &process.FactoryEntry{
			Factory: func() (process.Process, error) {
				return m.createProcess(id)
			},
			Meta: process.Meta{
				Method: method,
			},
		},
	})

	result := waiter.Wait()
	if !result.Accepted {
		return api.NewRegisterFactoryError(result.Error)
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

// createProcess creates a new process instance.
func (m *Manager) createProcess(id registry.ID) (process.Process, error) {
	compiled, err := m.code.Compile(id, processBuildOptions())
	if err != nil {
		return nil, api.NewCompileError(err)
	}

	return createProcess(compiled)
}

// createProcess creates a process from compiled code.
func createProcess(compiled *code.CompiledMain) (process.Process, error) {
	binders := engine.CoreBinders()
	binders = append(binders, processmod.BindGlobal)

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

// processBuildOptions returns build options for processes.
func processBuildOptions() *code.BuildOptions {
	return code.NewBuildOptions().
		WithMode(code.AllowAll)
}
