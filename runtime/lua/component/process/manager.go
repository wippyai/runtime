package process

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/runtime/lua/component"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	"go.uber.org/zap"
)

var (
	processBuild  *code.BuildOptions
	processLayers component.Option
)

func init() {
	processBuild = code.NewBuildOptions().
		WithMode(code.AllowAll).
		WithDenied(registry.ID{Name: "subscribe"}).
		WithPreloaded(code.Preload{Name: "channel", ModuleID: registry.ID{Name: "channel"}}).
		WithPreloaded(code.Preload{Name: "process", ModuleID: registry.ID{Name: "process"}})

	processLayers = component.WithLayerInitializer(func() []engine.RunnerOption {
		channels := channel.NewChannelLayer()
		return []engine.RunnerOption{
			engine.WithLayer(channels),
			engine.WithLayer(async.NewAsyncLayer(channels, 32)),
			engine.WithLayer(subscribe.NewSubscribe(channels)),
			engine.WithLayer(coroutine.NewCoroutineLayer()),
		}
	})
}

// Manager handles Lua process components
type Manager struct {
	log     *zap.Logger
	code    *code.Manager
	bus     events.Bus
	configs sync.Map // map[registry.ID]*api.ProcessConfig
}

// NewProcessManager creates a new process manager instance
func NewProcessManager(log *zap.Logger, code *code.Manager, bus events.Bus) *Manager {
	return &Manager{
		log:  log,
		code: code,
		bus:  bus,
	}
}

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

	if err := m.code.AddNode(ctx, node, component.BuildImports(cfg.Import, cfg.Modules)); err != nil {
		return fmt.Errorf("failed to add process node: %w", err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.upsertPrototype(ctx, entry.ID); err != nil {
		return fmt.Errorf("failed to create prototype: %w", err)
	}

	m.log.Debug("added process", zap.String("id", entry.ID.String()))
	return nil
}

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

	if err := m.code.UpdateNode(ctx, node, component.BuildImports(cfg.Import, cfg.Modules)); err != nil {
		return fmt.Errorf("failed to update process node: %w", err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.upsertPrototype(ctx, entry.ID); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		return fmt.Errorf("failed to update prototype: %w", err)
	}

	m.log.Debug("updated process", zap.String("id", entry.ID.String()))
	return nil
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindProcess {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindProcess)
	}

	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return fmt.Errorf("failed to delete process node: %w", err)
	}

	m.configs.Delete(entry.ID)
	m.unregisterPrototype(ctx, entry.ID)

	m.log.Debug("deleted process", zap.String("id", entry.ID.String()))
	return nil
}

func (m *Manager) Invalidate(ctx context.Context, ids []registry.ID) {
	for _, id := range ids {
		m.log.Debug("invalidating process", zap.String("id", id.String()))

		if _, exists := m.configs.Load(id); exists {
			if err := m.upsertPrototype(ctx, id); err != nil {
				m.log.Error("failed to recreate prototype", zap.Error(err))
			}
		}
	}
}

// createRunner creates a new runner for a process
func (m *Manager) createRunner(id registry.ID) (*engine.Runner, string, error) {
	compiled, err := m.code.Compile(id, processBuild)
	if err != nil {
		return nil, "", fmt.Errorf("failed to compile process: %w", err)
	}

	fvm, err := component.NewRunnerFactory(m.log, compiled, processLayers)
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

// upsertPrototype creates or updates a process prototype
func (m *Manager) upsertPrototype(ctx context.Context, id registry.ID) error {
	_, _, err := m.createRunner(id)
	if err != nil {
		return err
	}

	m.registerPrototype(ctx, id)
	return nil
}

// registerPrototype registers a process prototype
func (m *Manager) registerPrototype(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, events.Event{
		System: process.PrototypeSystem,
		Kind:   process.RegisterPrototype,
		Path:   id.String(),
		Data: process.Prototype(func() (process.Process, error) {
			runner, funcName, err := m.createRunner(id)
			if err != nil {
				return nil, err
			}

			return NewProcess(m.log, runner, funcName)
		}),
	})
}

// unregisterPrototype removes a process prototype registration
func (m *Manager) unregisterPrototype(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, events.Event{
		System: process.PrototypeSystem,
		Kind:   process.DeletePrototype,
		Path:   id.String(),
	})
}
