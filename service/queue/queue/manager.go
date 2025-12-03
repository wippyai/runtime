package queue

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	queuecfg "github.com/wippyai/runtime/api/service/queue/queue"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

type Manager struct {
	bus      event.Bus
	queueMgr queueapi.Manager
	dtt      payload.Transcoder
	logger   *zap.Logger
	queues   sync.Map
}

func NewManager(
	bus event.Bus,
	queueMgr queueapi.Manager,
	dtt payload.Transcoder,
	logger *zap.Logger,
) *Manager {
	return &Manager{
		bus:      bus,
		queueMgr: queueMgr,
		dtt:      dtt,
		logger:   logger,
	}
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	return m.addOrUpdateQueue(ctx, entry, "declared")
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if err := m.deleteQueue(ctx, entry.ID); err != nil {
		return err
	}
	return m.addOrUpdateQueue(ctx, entry, "updated")
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	return m.deleteQueue(ctx, entry.ID)
}

func (m *Manager) addOrUpdateQueue(ctx context.Context, entry registry.Entry, action string) error {
	cfg, err := entryutil.DecodeEntryConfig[queuecfg.Config](ctx, m.dtt, entry)
	if err != nil {
		m.logger.Error("failed to decode queue config",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
		return newConfigDecodeError(err)
	}

	if err := cfg.Validate(); err != nil {
		m.logger.Error("invalid queue config",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
		return newConfigValidationError(err)
	}

	if _, ok := m.queueMgr.GetDriver(cfg.Driver); !ok {
		m.logger.Error("driver not found for queue",
			zap.String("id", entry.ID.String()),
			zap.String("driver", cfg.Driver.String()))
		return newDriverNotFoundError(cfg.Driver)
	}

	queue := &queueapi.Queue{
		ID:       entry.ID,
		DriverID: cfg.Driver,
		Name:     entry.ID.Name,
		Options:  cfg.Options,
	}

	m.queues.Store(entry.ID, queue)

	m.bus.Send(ctx, event.Event{
		System: queueapi.System,
		Kind:   queueapi.QueueDeclare,
		Path:   entry.ID.String(),
		Data:   queue,
	})

	m.logger.Info("queue "+action,
		zap.String("id", entry.ID.String()),
		zap.String("driver", cfg.Driver.String()))

	return nil
}

func (m *Manager) deleteQueue(ctx context.Context, id registry.ID) error {
	m.queues.Delete(id)

	m.bus.Send(ctx, event.Event{
		System: queueapi.System,
		Kind:   queueapi.QueueDelete,
		Path:   id.String(),
	})

	m.logger.Info("queue deleted", zap.String("id", id.String()))

	return nil
}
