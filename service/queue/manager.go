// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/event"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/service/queue/interceptor"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type Manager struct {
	ctx          context.Context
	bus          event.Bus
	logger       *zap.Logger
	subscriber   *eventbus.Subscriber
	interceptors *interceptor.Registry
	drivers      sync.Map
	queues       sync.Map
	mu           sync.RWMutex
}

func NewManager(bus event.Bus, logger *zap.Logger) *Manager {
	if logger == nil {
		logger = zap.NewNop()
	}
	m := &Manager{
		bus:    bus,
		logger: logger,
	}
	m.interceptors = interceptor.NewRegistry(logger, m.publishDirect)
	return m
}

func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	m.ctx = ctx
	m.mu.Unlock()

	sub, err := eventbus.NewSubscriber(
		ctx,
		m.bus,
		queueapi.System,
		"queue.(driver|queue).(register|declare|delete)",
		m.handleEvent,
	)
	if err != nil {
		return NewConfigError("failed to create queue event subscriber", err)
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

func (m *Manager) handleEvent(e event.Event) {
	switch e.Kind {
	case queueapi.DriverRegister:
		m.handleDriverRegister(e)
	case queueapi.DriverDelete:
		m.handleDriverDelete(e)
	case queueapi.Declare:
		m.handleQueueDeclare(e)
	case queueapi.Delete:
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

	m.mu.RLock()
	ctx := m.ctx
	m.mu.RUnlock()

	if ctx == nil {
		ctx = context.Background()
	}

	if err := driver.DeclareQueue(ctx, queueEntry.ID, queueEntry.Config); err != nil {
		m.logger.Error("failed to declare queue on driver",
			zap.String("path", e.Path),
			zap.Error(err))
		m.sendReject(e.Path, NewConfigError("failed to declare queue", err).Error())
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
	return m.interceptors.Publish(ctx, q, msgs...)
}

func (m *Manager) publishDirect(ctx context.Context, q registry.ID, msgs ...*queueapi.Message) error {
	queueVal, ok := m.queues.Load(q)
	if !ok {
		return queueapi.ErrQueueNotFound
	}

	queue, ok := queueVal.(*queueapi.Queue)
	if !ok {
		m.logger.Error("queue has invalid type",
			zap.String("queue", q.String()),
			zap.String("type", fmt.Sprintf("%T", queueVal)))
		return NewConfigError("queue has invalid type: "+fmt.Sprintf("%T", queueVal), nil)
	}

	driverVal, ok := m.drivers.Load(queue.DriverID)
	if !ok {
		return queueapi.ErrDriverNotFound
	}

	driver, ok := driverVal.(queueapi.Driver)
	if !ok {
		m.logger.Error("driver has invalid type",
			zap.String("driver", queue.DriverID.String()),
			zap.String("type", fmt.Sprintf("%T", driverVal)))
		return NewConfigError("driver has invalid type: "+fmt.Sprintf("%T", driverVal), nil)
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

func (m *Manager) RegisterInterceptor(name string, interceptor queueapi.PublishInterceptor, priority int) {
	m.interceptors.Register(name, interceptor, priority)
}

func (m *Manager) UnregisterInterceptor(name string) {
	m.interceptors.Unregister(name)
}

func (m *Manager) sendAccept(path event.Path) {
	m.mu.RLock()
	ctx := m.ctx
	m.mu.RUnlock()

	if ctx == nil {
		ctx = context.Background()
	}

	m.bus.Send(ctx, event.Event{
		System: queueapi.System,
		Kind:   "queue.accept",
		Path:   path,
	})
}

func (m *Manager) sendReject(path event.Path, reason string) {
	m.mu.RLock()
	ctx := m.ctx
	m.mu.RUnlock()

	if ctx == nil {
		ctx = context.Background()
	}

	m.bus.Send(ctx, event.Event{
		System: queueapi.System,
		Kind:   "queue.reject",
		Path:   path,
		Data:   reason,
	})
}
