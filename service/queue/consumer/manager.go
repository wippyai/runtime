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
	mu        sync.RWMutex
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
	m.mu.Lock()
	defer m.mu.Unlock()

	// Decode config
	cfg, err := entryutil.DecodeEntryConfig[consumerapi.Config](ctx, m.dtt, entry)
	if err != nil {
		m.logger.Error("failed to decode consumer config",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
		return newConfigDecodeError(err)
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		m.logger.Error("invalid consumer config",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
		return newConfigValidationError(err)
	}

	// Validate queue exists
	queue, ok := m.queueMgr.GetQueue(cfg.Queue)
	if !ok {
		m.logger.Error("queue not found for consumer",
			zap.String("id", entry.ID.String()),
			zap.String("queue", cfg.Queue.String()))
		return newQueueNotFoundError(cfg.Queue)
	}

	// Get driver from queue
	driver, ok := m.queueMgr.GetDriver(queue.DriverID)
	if !ok {
		m.logger.Error("driver not found for queue",
			zap.String("id", entry.ID.String()),
			zap.String("queue", cfg.Queue.String()),
			zap.String("driver", queue.DriverID.String()))
		return newDriverNotFoundError(queue.DriverID)
	}

	// Validate function exists (basic check - registry will validate fully)
	// Function validation happens when consumer actually calls it

	// Create consumer instance
	consumer := NewConsumer(
		entry.ID,
		cfg,
		driver,
		m.funcReg,
		m.logger.Named("consumer"),
	)

	// Store consumer
	m.consumers.Store(entry.ID, consumer)

	// Register with supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: consumer,
			Config:  cfg.Lifecycle,
		},
	})

	m.logger.Info("consumer registered",
		zap.String("id", entry.ID.String()),
		zap.String("queue", cfg.Queue.String()),
		zap.String("func", cfg.Func.String()))

	return nil
}

// Update handles consumer configuration updates
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Delete old consumer
	m.deleteConsumer(ctx, entry.ID)

	// Decode config
	cfg, err := entryutil.DecodeEntryConfig[consumerapi.Config](ctx, m.dtt, entry)
	if err != nil {
		m.logger.Error("failed to decode consumer config",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
		return newConfigDecodeError(err)
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		m.logger.Error("invalid consumer config",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
		return newConfigValidationError(err)
	}

	// Validate queue exists
	queue, ok := m.queueMgr.GetQueue(cfg.Queue)
	if !ok {
		m.logger.Error("queue not found for consumer",
			zap.String("id", entry.ID.String()),
			zap.String("queue", cfg.Queue.String()))
		return newQueueNotFoundError(cfg.Queue)
	}

	// Get driver from queue
	driver, ok := m.queueMgr.GetDriver(queue.DriverID)
	if !ok {
		m.logger.Error("driver not found for queue",
			zap.String("id", entry.ID.String()),
			zap.String("queue", cfg.Queue.String()),
			zap.String("driver", queue.DriverID.String()))
		return newDriverNotFoundError(queue.DriverID)
	}

	// Create consumer instance
	consumer := NewConsumer(
		entry.ID,
		cfg,
		driver,
		m.funcReg,
		m.logger.Named("consumer"),
	)

	// Store consumer
	m.consumers.Store(entry.ID, consumer)

	// Register with supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: consumer,
			Config:  cfg.Lifecycle,
		},
	})

	m.logger.Info("consumer updated",
		zap.String("id", entry.ID.String()),
		zap.String("queue", cfg.Queue.String()),
		zap.String("func", cfg.Func.String()))

	return nil
}

// Delete handles consumer removal
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.deleteConsumer(ctx, entry.ID)
	return nil
}

// deleteConsumer removes a consumer (internal, assumes lock is held)
func (m *Manager) deleteConsumer(ctx context.Context, id registry.ID) {
	// Remove from map
	m.consumers.Delete(id)

	// Unregister from supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   id.String(),
	})

	m.logger.Info("consumer deleted", zap.String("id", id.String()))
}
