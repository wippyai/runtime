package env

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

type Registry struct {
	ctx        context.Context
	log        *zap.Logger
	bus        event.Bus
	storages   sync.Map // map[event.Path]env.Storage
	variables  sync.Map // map[name]env.Variable
	subscriber *eventbus.Subscriber
}

func NewRegistry(bus event.Bus, log *zap.Logger) *Registry {
	return &Registry{
		log: log,
		bus: bus,
	}
}

func (s *Registry) Start(ctx context.Context) error {
	s.ctx = ctx
	subscriber, err := eventbus.NewSubscriber(ctx, s.bus, env.System, "env.*", s.handleEvent)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	s.subscriber = subscriber
	return nil
}

func (s *Registry) Stop() error {
	if s.subscriber != nil {
		s.bus.Unsubscribe(s.ctx, s.subscriber.ID())
	}
	return nil
}

func (s *Registry) handleEvent(e event.Event) {
	switch e.Kind {
	case env.StorageRegister:
		s.registerStorage(e)
	case env.StorageDelete:
		s.deleteStorage(e)
	case registry.Accept, registry.Reject:
		// nothing, self emitted
	default:
		s.log.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (s *Registry) registerStorage(e event.Event) {
	storage, ok := e.Data.(env.Storage)
	if !ok {
		s.log.Error("invalid storage payload",
			zap.String("storage", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		s.sendReject(e.Path, "invalid storage data type")
		return
	}

	s.storages.Store(e.Path, storage)

	variables, err := storage.List(s.ctx)
	if err != nil {
		s.log.Error("failed to list storage variables",
			zap.String("storage", e.Path),
			zap.Error(err))
		s.sendReject(e.Path, "failed to list storage variables")
		return
	}

	for name, value := range variables {
		s.variables.Store(name, env.Variable{
			Name:         name,
			EnvName:      name,
			DefaultValue: value,
			StorageID:    e.Path,
			Meta:         registry.Metadata{}, // TODO
			ReadOnly:     true,                // TODO
		})
	}

	s.log.Debug("storage registered", zap.String("storage", e.Path))
	s.sendAccept(e.Path)
}

func (s *Registry) deleteStorage(e event.Event) {
	if _, exists := s.storages.LoadAndDelete(e.Path); !exists {
		s.log.Warn("storage not found", zap.String("storage", e.Path))
		s.sendReject(e.Path, "storage not found")
		return
	}

	// Delete all variables associated with this storage
	s.variables.Range(func(key, value interface{}) bool {
		v := value.(env.Variable)
		if v.StorageID == e.Path {
			s.variables.Delete(key)
		}
		return true
	})

	s.log.Debug("storage removed", zap.String("storage", e.Path))
	s.sendAccept(e.Path)
}

func (s *Registry) sendAccept(path event.Path) {
	s.bus.Send(s.ctx, event.Event{
		System: env.System,
		Kind:   registry.Accept,
		Path:   path,
	})
}

func (s *Registry) sendReject(path event.Path, reason string) {
	s.bus.Send(s.ctx, event.Event{
		System: env.System,
		Kind:   registry.Reject,
		Path:   path,
		Data:   reason,
	})
}

//// GetStorage retrieves an env storage by name
//func (s *Registry) GetStorage(ctx context.Context, name string) (env.Storage, error) {
//	storage, ok := s.storages.Load(name)
//	if !ok {
//		return nil, env.ErrStorageNotFound
//	}
//	return storage.(env.Storage), nil
//}

// All returns all env storages
func (s *Registry) All(ctx context.Context) ([]env.Storage, error) {
	var storages []env.Storage
	s.storages.Range(func(key, value interface{}) bool {
		storages = append(storages, value.(env.Storage))
		return true
	})
	return storages, nil
}

// Get retrieves an environment variable by name from a specific storage
func (s *Registry) Get(ctx context.Context, name string) (string, error) {
	// storage, err := s.GetStorage(ctx, storageName)
	// if err != nil {
	// 	return "", fmt.Errorf("failed to get storage %s: %w", storageName, err)
	// }

	var value string
	var found bool

	s.storages.Range(func(key, value interface{}) bool {
		storage := value.(env.Storage)
		v, err := storage.Get(ctx, name)
		if err == nil && v != "" {
			value = v
			found = true
			return false // Stop iteration after finding value
		}
		return true // Continue iteration if not found
	})

	if !found {
		return "", env.ErrVariableNotFound
	}

	// value, err := storage.Get(ctx, name)
	// if err != nil {
	// 	return "", fmt.Errorf("failed to get variable %s from storage %s: %w", name, storageName, err)
	// }

	return value, nil
}
