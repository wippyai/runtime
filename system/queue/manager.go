package queue

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/event"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// Note: fmt kept for Sprintf in logging

type Manager struct {
	ctx        context.Context
	bus        event.Bus
	logger     *zap.Logger
	drivers    sync.Map
	queues     sync.Map
	subscriber *eventbus.Subscriber
	chain      queueapi.PublishChain
	mu         sync.RWMutex
}

func NewManager(bus event.Bus, logger *zap.Logger) *Manager {
	return &Manager{
		bus:    bus,
		logger: logger,
	}
}

func (m *Manager) Start(ctx context.Context) error {
	m.ctx = ctx

	sub, err := eventbus.NewSubscriber(
		ctx,
		m.bus,
		queueapi.System,
		"queue.(driver|queue).(register|declare|delete)",
		m.handleEvent,
	)
	if err != nil {
		return NewSubscriberError(err)
	}
	m.subscriber = sub

	m.logger.Debug("queue manager started")
	return nil
}

func (m *Manager) Stop() error {
	if m.subscriber != nil {
		m.subscriber.Close()
	}

	m.logger.Debug("queue manager stopped")
	return nil
}

func (m *Manager) SetPublishChain(chain queueapi.PublishChain) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chain = chain
}

func (m *Manager) handleEvent(e event.Event) {
	switch e.Kind {
	case queueapi.DriverRegister:
		m.handleDriverRegister(e)
	case queueapi.DriverDelete:
		m.handleDriverDelete(e)
	case queueapi.QueueDeclare:
		m.handleQueueDeclare(e)
	case queueapi.QueueDelete:
		m.handleQueueDelete(e)
	default:
		m.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (m *Manager) handleDriverRegister(e event.Event) {
	driver, ok := e.Data.(queueapi.DriverService)
	if !ok {
		m.logger.Error("invalid driver payload",
			zap.String("path", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		m.sendReject(e.Path, "invalid driver type")
		return
	}

	id := registry.ParseID(e.Path)
	m.drivers.Store(id, driver)
	m.logger.Debug("driver registered", zap.String("path", e.Path))
	m.sendAccept(e.Path)
}

func (m *Manager) handleDriverDelete(e event.Event) {
	id := registry.ParseID(e.Path)
	m.drivers.Delete(id)

	m.logger.Debug("driver deleted", zap.String("path", e.Path))
	m.sendAccept(e.Path)
}

func (m *Manager) handleQueueDeclare(e event.Event) {
	queueEntry, ok := e.Data.(*queueapi.Queue)
	if !ok {
		m.logger.Error("invalid queue payload",
			zap.String("path", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		m.sendReject(e.Path, "invalid queue type")
		return
	}

	driverVal, ok := m.drivers.Load(queueEntry.DriverID)
	if !ok {
		m.logger.Error("driver not found for queue",
			zap.String("path", e.Path),
			zap.String("driver", queueEntry.DriverID.String()))
		m.sendReject(e.Path, NewDriverNotFoundError(queueEntry.DriverID).Error())
		return
	}

	driver, ok := driverVal.(queueapi.Driver)
	if !ok {
		m.logger.Error("driver has invalid type",
			zap.String("path", e.Path),
			zap.String("type", fmt.Sprintf("%T", driverVal)))
		m.sendReject(e.Path, "driver has invalid type")
		return
	}

	if err := driver.DeclareQueue(m.ctx, queueEntry.ID, queueEntry.Options); err != nil {
		m.logger.Error("failed to declare queue on driver",
			zap.String("path", e.Path),
			zap.Error(err))
		m.sendReject(e.Path, NewDeclareQueueError(err).Error())
		return
	}

	m.queues.Store(queueEntry.ID, queueEntry)
	m.logger.Debug("queue declared", zap.String("path", e.Path))
	m.sendAccept(e.Path)
}

func (m *Manager) handleQueueDelete(e event.Event) {
	id := registry.ParseID(e.Path)
	m.queues.Delete(id)
	m.logger.Debug("queue deleted", zap.String("path", e.Path))
	m.sendAccept(e.Path)
}

func (m *Manager) Publish(ctx context.Context, q registry.ID, msgs ...*queueapi.Message) error {
	m.mu.RLock()
	chain := m.chain
	m.mu.RUnlock()

	if chain != nil {
		return chain.Publish(ctx, q, msgs...)
	}

	return m.PublishDirect(ctx, q, msgs...)
}

func (m *Manager) PublishDirect(ctx context.Context, q registry.ID, msgs ...*queueapi.Message) error {
	queueVal, ok := m.queues.Load(q)
	if !ok {
		return queueapi.ErrNoQueue
	}

	queue, ok := queueVal.(*queueapi.Queue)
	if !ok {
		m.logger.Error("queue has invalid type",
			zap.String("queue", q.String()),
			zap.String("type", fmt.Sprintf("%T", queueVal)))
		return NewInvalidQueueTypeError(q, fmt.Sprintf("%T", queueVal))
	}

	driverVal, ok := m.drivers.Load(queue.DriverID)
	if !ok {
		return queueapi.ErrNoDriver
	}

	driver, ok := driverVal.(queueapi.Driver)
	if !ok {
		m.logger.Error("driver has invalid type",
			zap.String("driver", queue.DriverID.String()),
			zap.String("type", fmt.Sprintf("%T", driverVal)))
		return NewInvalidDriverTypeError(queue.DriverID, fmt.Sprintf("%T", driverVal))
	}

	return driver.Publish(ctx, q, msgs...)
}

func (m *Manager) GetDriver(id registry.ID) (queueapi.Driver, bool) {
	val, ok := m.drivers.Load(id)
	if !ok {
		return nil, false
	}
	driver, ok := val.(queueapi.Driver)
	return driver, ok
}

func (m *Manager) GetQueue(id registry.ID) (*queueapi.Queue, bool) {
	val, ok := m.queues.Load(id)
	if !ok {
		return nil, false
	}
	queue, ok := val.(*queueapi.Queue)
	return queue, ok
}

func (m *Manager) sendAccept(path event.Path) {
	m.bus.Send(m.ctx, event.Event{
		System: queueapi.System,
		Kind:   event.Kind("queue.accept"),
		Path:   path,
	})
}

func (m *Manager) sendReject(path event.Path, reason string) {
	m.bus.Send(m.ctx, event.Event{
		System: queueapi.System,
		Kind:   event.Kind("queue.reject"),
		Path:   path,
		Data:   reason,
	})
}
