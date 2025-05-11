package workflow

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/runtime/lua/component"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	"go.uber.org/zap"
)

var (
	workflowBuild *code.BuildOptions
	layers        component.Option
)

func init() {
	workflowBuild = code.NewBuildOptions().
		WithMode(code.DenyAll).
		WithPreloaded(
			code.Preload{Name: "channel", ModuleID: registry.ID{Name: "channel"}},
			code.Preload{Name: "workflow", ModuleID: registry.ID{Name: "workflow"}},
			code.Preload{Name: "payload", ModuleID: registry.ID{Name: "payload"}},
		)

	layers = component.WithRunnerOption(
		engine.WithLayer(channel.NewChannelLayer()),
		engine.WithLayer(subscribe.NewSubscribeLayer()),
		engine.WithLayer(coroutine.NewCoroutineLayer()),
	)
}

// Manager handles workflow components
type Manager struct {
	log     *zap.Logger
	code    *code.Manager
	bus     event.Bus
	configs sync.Map // map[registry.ID]*api.WorkflowConfig
}

// NewManager creates a new workflow manager instance
func NewManager(log *zap.Logger, code *code.Manager, bus event.Bus) *Manager {
	return &Manager{
		log:  log,
		code: code,
		bus:  bus,
	}
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindWorkflow {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindWorkflow)
	}

	cfg, err := component.UnpackConfig[api.WorkflowConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack workflow config: %w", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindWorkflow,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.AddNode(ctx, node, component.BuildImports(cfg.Imports, nil)); err != nil {
		return fmt.Errorf("failed to add workflow node: %w", err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.upsertPrototype(ctx, entry.ID); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		return fmt.Errorf("failed to create prototype: %w", err)
	}

	m.log.Debug("added workflow", zap.String("id", entry.ID.String()))
	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindWorkflow {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindWorkflow)
	}

	cfg, err := component.UnpackConfig[api.WorkflowConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack workflow config: %w", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindWorkflow,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.UpdateNode(ctx, node, component.BuildImports(cfg.Imports, nil)); err != nil {
		return fmt.Errorf("failed to update workflow node: %w", err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.upsertPrototype(ctx, entry.ID); err != nil {
		return fmt.Errorf("failed to update prototype: %w", err)
	}

	m.log.Debug("updated workflow", zap.String("id", entry.ID.String()))
	return nil
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindWorkflow {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindWorkflow)
	}

	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return fmt.Errorf("failed to delete workflow node: %w", err)
	}

	m.configs.Delete(entry.ID)
	m.unregisterPrototype(ctx, entry.ID)

	m.log.Debug("deleted workflow", zap.String("id", entry.ID.String()))
	return nil
}

func (m *Manager) Invalidate(ctx context.Context, ids []registry.ID) {
	for _, id := range ids {
		m.log.Debug("invalidating workflow", zap.String("id", id.String()))

		if _, exists := m.configs.Load(id); exists {
			if err := m.upsertPrototype(ctx, id); err != nil {
				m.log.Error("failed to recreate prototype", zap.Error(err))
			}
		}
	}
}

// createRunner creates a new runner for a workflow
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
	_, _, err := m.createRunner(id)
	if err != nil {
		return err
	}

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

			return NewLuaWorkflow(m.log, runner, funcName)
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
