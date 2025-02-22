package btea

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/pubsub"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/runtime/lua/component"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"go.uber.org/zap"
)

var (
	bteaBuild *code.BuildOptions
	layers    component.Option
)

func init() {
	bteaBuild = code.NewBuildOptions().
		WithMode(code.AllowAll).
		WithDenied(registry.ID{Name: "subscribe"}).
		WithPreloaded(code.Preload{Name: "channel", ModuleID: registry.ID{Name: "channel"}}).
		WithPreloaded(code.Preload{Name: "upstream", ModuleID: registry.ID{Name: "upstream"}}).
		WithPreloaded(code.Preload{Name: "tasks", ModuleID: registry.ID{Name: "tasks"}}).
		WithPreloaded(code.Preload{Name: "btea", ModuleID: registry.ID{Name: "btea"}}).
		WithPreloaded(code.Preload{Name: "process", ModuleID: registry.ID{Name: "process"}})

	layers = component.WithLayerInitializer(func() []engine.RunnerOption {
		channels := channel.NewChannelLayer()
		return []engine.RunnerOption{
			engine.WithLayer(channels),
			engine.WithLayer(async.NewAsyncLayer(channels, 32)),
			engine.WithLayer(pubsub.NewSubscribe(channels)),
			engine.WithLayer(coroutine.NewCoroutineLayer()),
		}
	})
}

// Manager is responsible for handling Btea apps.
type Manager struct {
	log     *zap.Logger
	code    *code.Manager
	bus     events.Bus
	configs sync.Map // map[registry.ID]*api.BteaConfig
}

// NewBteaManager creates a new instance of Manager.
func NewBteaManager(log *zap.Logger, code *code.Manager, bus events.Bus) *Manager {
	return &Manager{
		log:  log,
		code: code,
		bus:  bus,
	}
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindBteaApp {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindBteaApp)
	}

	cfg, err := component.UnpackConfig[api.BteaConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack btea config: %w", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindBteaApp,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.AddNode(ctx, node, component.BuildImports(cfg.Import, cfg.Modules)); err != nil {
		return fmt.Errorf("failed to add btea node: %w", err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.upsertPrototype(ctx, entry.ID); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		return fmt.Errorf("failed to create prototype: %w", err)
	}

	m.log.Debug("added btea app", zap.String("id", entry.ID.String()))
	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindBteaApp {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindBteaApp)
	}

	cfg, err := component.UnpackConfig[api.BteaConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack btea config: %w", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindBteaApp,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.UpdateNode(ctx, node, component.BuildImports(cfg.Import, nil)); err != nil {
		return fmt.Errorf("failed to update btea node: %w", err)
	}

	m.configs.Store(entry.ID, cfg)

	if err := m.upsertPrototype(ctx, entry.ID); err != nil {
		return fmt.Errorf("failed to update prototype: %w", err)
	}

	m.log.Debug("updated btea app", zap.String("id", entry.ID.String()))
	return nil
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindBteaApp {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindBteaApp)
	}

	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return fmt.Errorf("failed to delete btea node: %w", err)
	}

	m.configs.Delete(entry.ID)
	m.unregisterPrototype(ctx, entry.ID)

	m.log.Debug("deleted btea app", zap.String("id", entry.ID.String()))
	return nil
}

func (m *Manager) Invalidate(ctx context.Context, ids []registry.ID) {
	for _, id := range ids {
		m.log.Debug("invalidating btea app", zap.String("id", id.String()))

		if _, exists := m.configs.Load(id); exists {
			if err := m.upsertPrototype(ctx, id); err != nil {
				m.log.Error("failed to recreate prototype", zap.Error(err))
			}
		}
	}
}

// createRunner creates a new runner for a btea app
func (m *Manager) createRunner(id registry.ID) (*engine.Runner, string, error) {
	compiled, err := m.code.Compile(id, bteaBuild)
	if err != nil {
		return nil, "", fmt.Errorf("failed to compile btea app: %w", err)
	}

	fvm, err := component.NewRunnerFactory(m.log, compiled, layers)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create runner factory: %w", err)
	}

	defer func() {
		err := fvm.Close()
		if err != nil {
			m.log.Error("failed to close runner factory", zap.Error(err))
		}
	}()

	runner, err := fvm.CreateRunner()
	if err != nil {
		return nil, "", fmt.Errorf("failed to create runner: %w", err)
	}

	return runner, compiled.FuncName, nil
}

// upsertPrototype creates or updates a btea app prototype
func (m *Manager) upsertPrototype(ctx context.Context, id registry.ID) error {
	_, _, err := m.createRunner(id)
	if err != nil {
		// compile check
		return err
	}

	m.registerPrototype(ctx, id)
	return nil
}

// registerPrototype registers a btea app as a process prototype
func (m *Manager) registerPrototype(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, events.Event{
		System: process.PrototypeSystem,
		Kind:   process.RegisterPrototype,
		Path:   id.String(),
		Data: process.Prototype(func() (process.Process, error) {
			runner, funcName, err := m.createRunner(id)
			if err != nil {
				// compile check
				return nil, err
			}

			return NewApp(m.log, payload.GetTranscoder(ctx), runner, funcName)
		}),
	})
}

// unregisterPrototype removes a btea app's process prototype registration
func (m *Manager) unregisterPrototype(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, events.Event{
		System: process.PrototypeSystem,
		Kind:   process.DeletePrototype,
		Path:   id.String(),
	})
}
