package consumer

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	consumerapi "github.com/wippyai/runtime/api/service/queue/consumer"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

// Manager manages queue consumers
type Manager struct {
	bus       event.Bus
	queueMgr  queueapi.Manager
	funcReg   function.Registry
	dtt       payload.Transcoder
	logger    *zap.Logger
	consumers sync.Map
}

// NewManager creates a new consumer manager
func NewManager(
	bus event.Bus,
	queueMgr queueapi.Manager,
	funcReg function.Registry,
	dtt payload.Transcoder,
	logger *zap.Logger,
) *Manager {
	return &Manager{
		bus:      bus,
		queueMgr: queueMgr,
		funcReg:  funcReg,
		dtt:      dtt,
		logger:   logger,
	}
}

// Add handles new consumer registry entries
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	return m.addOrUpdate(ctx, entry, "registered")
}

// Update handles consumer configuration updates
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	m.deleteConsumer(ctx, entry.ID)
	return m.addOrUpdate(ctx, entry, "updated")
}

func (m *Manager) addOrUpdate(ctx context.Context, entry registry.Entry, action string) error {
	cfg, err := entryutil.DecodeEntryConfig[consumerapi.Config](ctx, m.dtt, entry)
	if err != nil {
		m.logger.Error("failed to decode consumer config",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
		return queueapi.NewConfigError("failed to decode consumer config", err)
	}

	if err := cfg.Validate(); err != nil {
		m.logger.Error("invalid consumer config",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
		return queueapi.NewConfigError("invalid consumer config", err)
	}

	queue, ok := m.queueMgr.GetQueue(cfg.Queue)
	if !ok {
		m.logger.Error("queue not found for consumer",
			zap.String("id", entry.ID.String()),
			zap.String("queue", cfg.Queue.String()))
		return queueapi.NewQueueNotFoundError(cfg.Queue)
	}

	driver, ok := m.queueMgr.GetDriver(queue.DriverID)
	if !ok {
		m.logger.Error("driver not found for queue",
			zap.String("id", entry.ID.String()),
			zap.String("queue", cfg.Queue.String()),
			zap.String("driver", queue.DriverID.String()))
		return queueapi.NewDriverNotFoundError(queue.DriverID)
	}

	consumer := NewConsumer(
		entry.ID,
		cfg,
		driver,
		m.funcReg,
		m.logger.Named("consumer"),
	)

	m.consumers.Store(entry.ID, consumer)

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: consumer,
			Config:  cfg.Lifecycle,
		},
	})

	m.logger.Info("consumer "+action,
		zap.String("id", entry.ID.String()),
		zap.String("queue", cfg.Queue.String()),
		zap.String("func", cfg.Func.String()))

	return nil
}

// Delete handles consumer removal
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	m.deleteConsumer(ctx, entry.ID)
	return nil
}

func (m *Manager) deleteConsumer(ctx context.Context, id registry.ID) {
	m.consumers.Delete(id)

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   id.String(),
	})

	m.logger.Info("consumer deleted", zap.String("id", id.String()))
}
