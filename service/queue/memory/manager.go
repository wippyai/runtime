// SPDX-License-Identifier: MPL-2.0

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
	queuesvc "github.com/wippyai/runtime/service/queue"
	"go.uber.org/zap"
)

type Manager struct {
	dtt     payload.Transcoder
	bus     event.Bus
	log     *zap.Logger
	drivers map[registry.ID]*Driver
	mu      sync.RWMutex
}

func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Manager {
	if log == nil {
		log = zap.NewNop()
	}
	return &Manager{
		log:     log,
		dtt:     dtt,
		bus:     bus,
		drivers: make(map[registry.ID]*Driver),
	}
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != memoryapi.Kind {
		return queuesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.drivers[entry.ID]; exists {
		return queuesvc.NewDriverExistsError(entry.ID)
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

	if err := queuesvc.SendAndAwaitManagerAck(ctx, m.bus, event.Event{
		System: queueapi.System,
		Kind:   queueapi.DriverRegister,
		Path:   entry.ID.String(),
		Data:   driver,
	}, "queue driver register"); err != nil {
		delete(m.drivers, entry.ID)
		m.bus.Send(ctx, event.Event{
			System: supervisor.System,
			Kind:   supervisor.ServiceRemove,
			Path:   entry.ID.String(),
		})
		return err
	}

	m.log.Info("added memory driver", zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != memoryapi.Kind {
		return queuesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.RLock()
	driver, exists := m.drivers[entry.ID]
	m.mu.RUnlock()

	if !exists {
		return queuesvc.NewDriverNotFoundError(entry.ID)
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
		return queuesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.drivers[entry.ID]; !exists {
		return queuesvc.NewDriverNotFoundError(entry.ID)
	}

	delete(m.drivers, entry.ID)

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   entry.ID.String(),
	})

	if err := queuesvc.SendAndAwaitManagerAck(ctx, m.bus, event.Event{
		System: queueapi.System,
		Kind:   queueapi.DriverDelete,
		Path:   entry.ID.String(),
	}, "queue driver delete"); err != nil {
		return err
	}

	m.log.Info("deleted memory driver", zap.String("id", entry.ID.String()))

	return nil
}
