package terminal

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/runtime/lua/factory"
	"github.com/ponyruntime/pony/tests/stub_process"
	"go.uber.org/zap"
	"log"
	"sync"
)

type Manager struct {
	log     *zap.Logger
	code    *code.Manager
	bus     events.Bus
	configs sync.Map // map[registry.ID]*api.TerminalConfig
}

func NewTerminalManager(log *zap.Logger, code *code.Manager, bus events.Bus) *Manager {
	return &Manager{
		log:  log,
		code: code,
		bus:  bus,
	}
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindTerminal {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindTerminal)
	}

	cfg, err := factory.UnpackConfig[api.TerminalConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack terminal config: %w", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindTerminal,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.AddNode(ctx, node, factory.BuildImports(cfg.Import, nil)); err != nil {
		return fmt.Errorf("failed to add terminal node: %w", err)
	}

	m.configs.Store(entry.ID, cfg)

	// Register terminal prototype
	m.upsertPrototype(ctx, entry.ID)
	m.log.Debug("added terminal", zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindTerminal {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindTerminal)
	}

	cfg, err := factory.UnpackConfig[api.TerminalConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack terminal config: %w", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindTerminal,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	if err := m.code.UpdateNode(ctx, node, factory.BuildImports(cfg.Import, nil)); err != nil {
		return fmt.Errorf("failed to update terminal node: %w", err)
	}

	m.configs.Store(entry.ID, cfg)

	// Re-register terminal prototype on update
	m.upsertPrototype(ctx, entry.ID)
	m.log.Debug("updated terminal", zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindTerminal {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindTerminal)
	}

	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return fmt.Errorf("failed to delete terminal node: %w", err)
	}

	m.configs.Delete(entry.ID)

	// Unregister terminal prototype
	m.unregisterPrototype(ctx, entry.ID)
	m.log.Debug("deleted terminal", zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) Invalidate(ids []registry.ID) {
	for _, id := range ids {
		m.log.Debug("invalidating terminal", zap.String("id", id.String()))

		// Re-register prototype when terminal is invalidated
		if _, exists := m.configs.Load(id); exists {
			m.upsertPrototype(context.Background(), id)
		}
	}
}

// upsertPrototype registers a terminal as a process prototype
func (m *Manager) upsertPrototype(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, events.Event{
		System: process.PrototypeSystem,
		Kind:   process.RegisterPrototype,
		Path:   id.String(),
		Data: process.Prototype(func() (process.Process, error) {
			log.Printf("LAUNCHING SHIT! REWRITE IT!")
			return stub_process.NewTickerProcess(), nil
		}),
	})
}

// unregisterPrototype removes a terminal's process prototype registration
func (m *Manager) unregisterPrototype(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, events.Event{
		System: process.PrototypeSystem,
		Kind:   process.DeletePrototype,
		Path:   id.String(),
	})
}
