// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	amqpapi "github.com/wippyai/runtime/api/service/queue/amqp"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	queuesvc "github.com/wippyai/runtime/service/queue"
	"go.uber.org/zap"
)

// Manager handles lifecycle of AMQP driver instances.
type Manager struct {
	dtt     payload.Transcoder
	bus     event.Bus
	log     *zap.Logger
	drivers map[registry.ID]*Driver
	mu      sync.RWMutex
}

// NewManager creates a new AMQP driver manager.
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
	if entry.Kind != amqpapi.Kind {
		return queuesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.drivers[entry.ID]; exists {
		return queuesvc.NewDriverExistsError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[amqpapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	driver := NewDriver(entry.ID, cfg, m.dtt, m.log)
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

	m.log.Info("added amqp driver", zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != amqpapi.Kind {
		return queuesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.drivers[entry.ID]; !exists {
		return queuesvc.NewDriverNotFoundError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[amqpapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	// Tear the old driver down before swapping in the replacement: the
	// supervisor uses ServiceRemove to stop the service, and queue
	// consumers observe DriverDelete so they stop dispatching to the dying
	// instance. A plain ServiceUpdate would leave the old driver running
	// with stale config.
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

	driver := NewDriver(entry.ID, cfg, m.dtt, m.log)
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

	m.log.Info("updated amqp driver", zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != amqpapi.Kind {
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

	m.log.Info("deleted amqp driver", zap.String("id", entry.ID.String()))

	return nil
}
