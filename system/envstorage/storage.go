package envstorage

import (
	"context"
	"fmt"
	"sync"

	envstorageapi "github.com/ponyruntime/pony/api/envstorage"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

type Manager struct {
	ctx        context.Context
	log        *zap.Logger
	bus        event.Bus
	storages   sync.Map // map[event.Path]envstorageapi.Storage
	subscriber *eventbus.Subscriber
}

func NewManager(bus event.Bus, log *zap.Logger) *Manager {
	return &Manager{
		log: log,
		bus: bus,
	}
}

func (s *Manager) Start(ctx context.Context) error {
	s.ctx = ctx
	subscriber, err := eventbus.NewSubscriber(ctx, s.bus, envstorageapi.System, "", s.handleEvent)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	s.subscriber = subscriber
	return nil
}

func (s *Manager) Stop() error {
	if s.subscriber != nil {
		s.bus.Unsubscribe(s.ctx, s.subscriber.ID())
	}
	return nil
}

func (s *Manager) handleEvent(e event.Event) {
	switch e.Kind {
	case envstorageapi.Register:
		s.registerStorage(e)
	case envstorageapi.Delete:
		s.deleteStorage(e)
	case envstorageapi.Accept, envstorageapi.Reject:
		// nothing, self emitted
	default:
		s.log.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (s *Manager) registerStorage(e event.Event) {
	storage, ok := e.Data.(envstorageapi.Storage)
	if !ok {
		s.log.Error("invalid storage payload",
			zap.String("storage", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		s.sendReject(e.Path, "invalid storage data type")
		return
	}

	s.storages.Store(e.Path, storage)
	s.log.Debug("storage registered", zap.String("storage", e.Path))
	s.sendAccept(e.Path)
}

func (s *Manager) deleteStorage(e event.Event) {
	if _, exists := s.storages.LoadAndDelete(e.Path); !exists {
		s.log.Warn("storage not found", zap.String("storage", e.Path))
		s.sendReject(e.Path, "storage not found")
		return
	}

	s.log.Debug("storage removed", zap.String("storage", e.Path))
	s.sendAccept(e.Path)
}

func (s *Manager) sendAccept(path event.Path) {
	s.bus.Send(s.ctx, event.Event{
		System: envstorageapi.System,
		Kind:   envstorageapi.Accept,
		Path:   path,
	})
}

func (s *Manager) sendReject(path event.Path, reason string) {
	s.bus.Send(s.ctx, event.Event{
		System: envstorageapi.System,
		Kind:   envstorageapi.Reject,
		Path:   path,
		Data:   reason,
	})
}
