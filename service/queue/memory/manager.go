package memory

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	memoryapi "github.com/wippyai/runtime/api/service/queue/memory"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

type Manager struct {
	log     *zap.Logger
	dtt     payload.Transcoder
	bus     event.Bus
	mu      sync.RWMutex
	drivers map[registry.ID]*Driver
}

func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Manager {
	return &Manager{
		log:     log,
		dtt:     dtt,
		bus:     bus,
		drivers: make(map[registry.ID]*Driver),
	}
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != memoryapi.Kind {
		return newUnsupportedEntryKindError(string(entry.Kind))
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.drivers[entry.ID]; exists {
		return newDriverExistsError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[memoryapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	driver := NewDriver(entry.ID, m.log)
	m.drivers[entry.ID] = driver

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: driver,
			Config:  cfg.Lifecycle,
		},
	})

	m.bus.Send(ctx, event.Event{
		System: queueapi.System,
		Kind:   queueapi.DriverRegister,
		Path:   entry.ID.String(),
		Data:   driver,
	})

	m.log.Info("added memory driver", zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != memoryapi.Kind {
		return newUnsupportedEntryKindError(string(entry.Kind))
	}

	m.mu.RLock()
	driver, exists := m.drivers[entry.ID]
	m.mu.RUnlock()

	if !exists {
		return newDriverNotFoundError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[memoryapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceUpdate,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: driver,
			Config:  cfg.Lifecycle,
		},
	})

	m.log.Info("updated memory driver", zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != memoryapi.Kind {
		return newUnsupportedEntryKindError(string(entry.Kind))
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.drivers[entry.ID]; !exists {
		return newDriverNotFoundError(entry.ID)
	}

	delete(m.drivers, entry.ID)

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   entry.ID.String(),
	})

	m.bus.Send(ctx, event.Event{
		System: queueapi.System,
		Kind:   queueapi.DriverDelete,
		Path:   entry.ID.String(),
	})

	m.log.Info("deleted memory driver", zap.String("id", entry.ID.String()))

	return nil
}
