package workflow

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	baseprocess "github.com/wippyai/runtime/runtime/lua/component/process"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	"github.com/wippyai/runtime/runtime/lua/engine/subscribe"
	"go.uber.org/zap"
)

var (
	workflowBuild *code.BuildOptions

	layers component.Option
)

func init() {
	workflowBuild = code.NewBuildOptions().
		WithMode(code.AllowAll).
		WithPreloaded(code.Preload{Name: "upstream", ModuleID: registry.NewID("", "upstream")})

	layers = component.WithRunnerOption(
		engine.WithLayer(channel.NewChannelLayer()),
		engine.WithLayer(subscribe.NewSubscribeLayer()),
		engine.WithLayer(coroutine.NewCoroutineLayer()),
	)
}

// Manager handles workflow.lua component lifecycle
type Manager struct {
	log     *zap.Logger
	code    *code.Manager
	bus     event.Bus
	configs sync.Map
}

// NewManager creates a new workflow manager
func NewManager(log *zap.Logger, codeManager *code.Manager, bus event.Bus) *Manager {
	return &Manager{
		log:  log,
		code: codeManager,
		bus:  bus,
	}
}

// Add implements component.Manager - adds a new workflow
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	log.Printf("ASDDD")
	// Unpack workflow configuration
	cfg, err := component.UnpackConfig[luaapi.ProcessConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack workflow config: %w", err)
	}

	// Validate configuration
	if cfg.Source == "" {
		return fmt.Errorf("workflow source cannot be empty")
	}
	if cfg.Method == "" {
		return fmt.Errorf("workflow method cannot be empty")
	}

	// Create code node
	node := code.Node{
		ID:     entry.ID,
		Kind:   luaapi.KindWorkflow,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	// Add to code manager with imports
	err = m.code.AddNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules))
	if err != nil {
		return fmt.Errorf("failed to add workflow code node: %w", err)
	}

	// Store config for invalidation
	m.configs.Store(entry.ID, cfg)

	// Register prototype
	if err := m.upsertPrototype(ctx, entry.ID); err != nil {
		return fmt.Errorf("failed to register workflow prototype: %w", err)
	}

	m.log.Info("workflow added",
		zap.Stringer("id", entry.ID),
		zap.String("source", cfg.Source),
		zap.String("method", cfg.Method),
	)

	return nil
}

// Update implements component.Manager - updates existing workflow
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	// Unpack workflow configuration
	cfg, err := component.UnpackConfig[luaapi.ProcessConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack workflow config: %w", err)
	}

	// Update code node
	node := code.Node{
		ID:     entry.ID,
		Kind:   luaapi.KindWorkflow,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	err = m.code.UpdateNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules))
	if err != nil {
		return fmt.Errorf("failed to update workflow code node: %w", err)
	}

	// Store config for invalidation
	m.configs.Store(entry.ID, cfg)

	// Re-register prototype
	if err := m.upsertPrototype(ctx, entry.ID); err != nil {
		return fmt.Errorf("failed to re-register workflow prototype: %w", err)
	}

	m.log.Info("workflow updated",
		zap.Stringer("id", entry.ID),
		zap.String("source", cfg.Source),
		zap.String("method", cfg.Method),
	)

	return nil
}

// Delete implements registry.EntryListener - deletes workflow
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	err := m.code.DeleteNode(ctx, entry.ID)
	if err != nil {
		return fmt.Errorf("failed to delete workflow code node: %w", err)
	}

	// Remove config
	m.configs.Delete(entry.ID)

	// Unregister prototype
	m.unregisterPrototype(ctx, entry.ID)

	m.log.Info("workflow deleted", zap.Stringer("id", entry.ID))
	return nil
}

// Invalidate implements component.EntityHandler - invalidates cached workflow code
func (m *Manager) Invalidate(ctx context.Context, ids []registry.ID) {
	for _, id := range ids {
		if _, exists := m.configs.Load(id); exists {
			m.log.Debug("invalidating workflow", zap.String("id", id.String()))

			if err := m.upsertPrototype(ctx, id); err != nil {
				m.log.Error("failed to recreate prototype", zap.Error(err))
			}
		}
	}
}

// createRunner creates a runner for a workflow
func (m *Manager) createRunner(id registry.ID) (*engine.Runner, string, error) {
	compiled, err := m.code.Compile(id, workflowBuild)
	if err != nil {
		return nil, "", fmt.Errorf("failed to compile workflow: %w", err)
	}

	fvm, err := component.NewRunnerFactory(m.log, compiled, layers)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create runner factory: %w", err)
	}

	defer func() {
		if err := fvm.Close(); err != nil {
			m.log.Error("failed to close runner factory", zap.Error(err))
		}
	}()

	runner, err := fvm.CreateRunner()
	if err != nil {
		return nil, "", fmt.Errorf("failed to create runner: %w", err)
	}

	return runner, compiled.FuncName, nil
}

// upsertPrototype creates or updates a workflow prototype
func (m *Manager) upsertPrototype(ctx context.Context, id registry.ID) error {
	m.registerPrototype(ctx, id)
	return nil
}

// registerPrototype registers a workflow prototype
func (m *Manager) registerPrototype(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: process.PrototypeSystem,
		Kind:   process.ProtoRegister,
		Path:   id.String(),
		Data: process.Prototype(func() (process.Process, error) {
			runner, funcName, err := m.createRunner(id)
			if err != nil {
				return nil, err
			}

			state, err := baseprocess.NewState(m.log, runner, funcName)
			if err != nil {
				return nil, err
			}

			return NewLuaWorkflow(m.log, state), nil
		}),
	})
}

// unregisterPrototype removes a workflow prototype registration
func (m *Manager) unregisterPrototype(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: process.PrototypeSystem,
		Kind:   process.ProtoDelete,
		Path:   id.String(),
	})
}
