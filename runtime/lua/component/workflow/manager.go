// Package workflow provides Lua workflow management.
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
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// workflowAllowedIDs are the modules allowed in workflow execution.
var workflowAllowedIDs = []registry.ID{
	{Name: "json"},
	{Name: "base64"},
	{Name: "payload"},
	{Name: "workflow"},
	{Name: "channel"},
}

// Manager handles Lua workflow components for engine2.
// Workflows have restricted module access compared to processes.
type Manager struct {
	log     *zap.Logger
	code    *code.Manager
	bus     event.Bus
	factory engine.CompiledFactory
	awaiter *eventbus.Awaiter
	configs sync.Map // map[registry.ID]*api.WorkflowConfig
}

// NewManager creates a new workflow manager.
func NewManager(log *zap.Logger, code *code.Manager, bus event.Bus, factory engine.CompiledFactory) *Manager {
	return &Manager{
		log:     log,
		code:    code,
		bus:     bus,
		factory: factory,
		awaiter: eventbus.NewAwaiter(bus, process.System, "factory.(accept|reject)"),
	}
}

// Add implements registry.EntryListener.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindWorkflow {
		return api.NewInvalidEntryKindError(entry.Kind, api.KindWorkflow)
	}

	cfg, err := component.UnpackConfig[api.WorkflowConfig](ctx, entry)
	if err != nil {
		return api.NewUnpackConfigError("workflow", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindWorkflow,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.AddNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		return api.NewAddNodeError("workflow", err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.registerFactory(ctx, entry.ID, cfg.Method); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		m.configs.Delete(entry.ID)
		return api.NewRegisterFactoryError(err)
	}

	m.log.Debug("added workflow", zap.String("id", entry.ID.String()))
	return nil
}

// Update implements registry.EntryListener.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindWorkflow {
		return api.NewInvalidEntryKindError(entry.Kind, api.KindWorkflow)
	}

	cfg, err := component.UnpackConfig[api.WorkflowConfig](ctx, entry)
	if err != nil {
		return api.NewUnpackConfigError("workflow", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindWorkflow,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.UpdateNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		return api.NewUpdateNodeError("workflow", err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.registerFactory(ctx, entry.ID, cfg.Method); err != nil {
		return api.NewUpdateFactoryError(err)
	}

	m.log.Debug("updated workflow", zap.String("id", entry.ID.String()))
	return nil
}

// Delete implements registry.EntryListener.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindWorkflow {
		return api.NewInvalidEntryKindError(entry.Kind, api.KindWorkflow)
	}

	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return api.NewDeleteNodeError("workflow", err)
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
	// Create factory using ProcessFactory with workflow-specific restrictions
	factoryFn, err := m.factory.CreateFactory(id,
		engine.WithMode(code.AllowListed),
		engine.WithAllowed(append(workflowAllowedIDs, id)...),
		engine.WithoutDefaultModule("process"), // Workflows don't get process module
	)
	if err != nil {
		return api.NewCompileError(err)
	}

	if method == "" {
		method = "main"
	}

	path := id.String()

	waiter, err := m.awaiter.Prepare(ctx, path)
	if err != nil {
		return api.NewRegisterFactoryError(err)
	}

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

// Compile-time check
var _ registry.EntryListener = (*Manager)(nil)
