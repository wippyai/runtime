package env

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/pubsub"

	"github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

type EnvValue struct {
	ID           registry.ID
	VariableID   string
	StorageID    string
	Name         string
	Value        string
	DefaultValue string
	ReadOnly     bool
}

type Registry struct {
	ctx        context.Context
	log        *zap.Logger
	bus        event.Bus
	storages   sync.Map // map[event.Path]env.Storage
	variables  sync.Map // map[name]env.Variable
	values     sync.Map // map[registry.ID]EnvValue
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
	s.log.Info("received event -- ", zap.Any("event", e))

	switch e.Kind {
	case env.StorageRegister:
		s.registerStorage(e)
	case env.StorageDelete:
		s.deleteStorage(e)
	case env.VariableRegister:
		s.registerVariable(e)
	case env.VariableDelete:
		// TODO
	case env.VariableUpdate:
		// TODO
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

func (s *Registry) registerVariable(e event.Event) {
	// 2025-05-12 18:29:54     INFO    env     received event --       {"event": {"System":"env","Kind":"env.variableregister","Path":"app.env.demo:openai_api_url","Data":{"meta":{"comment":"ENV Demo Application","depends_on":["app.env.demo:envmemory"],"type":"secret_key"},"name":"openai_api_url","variable":"OPENAI_API_URL","readonly":true,"storage":"app.env.demo:envmemory"}}}
	s.log.Info("registerVariable. received register variable", zap.String("variable", e.Path))

	variable, ok := e.Data.(env.Variable)
	if !ok {
		s.log.Error("invalid variable payload",
			zap.String("type", fmt.Sprintf("%T", e.Data)),
			zap.Any("event", e))
		s.sendReject(e.Path, "invalid variable data type")
		return
	}

	s.variables.Store(variable.Name, variable)
	s.log.Debug("variable registered",
		zap.String("name", variable.Name),
		zap.String("storage", variable.StorageID))

	variableID := registry.ParseID(e.Path)
	storageID := registry.ParseID(variable.StorageID)
	variableName := variable.EnvName

	storedStorage, found := s.storages.Load(storageID.String())
	if !found {
		s.log.Error("storage not found", zap.String("storage", storageID.String()))
		return
	}

	storage, ok := storedStorage.(env.Storage)
	if !ok {
		s.log.Error("invalid storage payload", zap.String("storage", storageID.String()))
		return
	}

	variableValue, err := storage.Get(context.Background(), variableName)
	if err != nil {
		s.log.Error("storage not found", zap.String("storage", storageID.String()))
		return
	}

	value := EnvValue{
		ID:           variableID,
		VariableID:   variableID.String(),
		StorageID:    storageID.String(),
		Name:         variable.EnvName,
		Value:        variableValue,
		DefaultValue: variable.DefaultValue,
		ReadOnly:     variable.ReadOnly,
	}

	s.values.Store(variableID, value)

	s.log.Info("registerVariable. value stored", zap.Any("id", variableID), zap.Any("value", value))

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
// func (s *Registry) GetStorage(ctx context.Context, name string) (env.Storage, error) {
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
	s.log.Info("getting environment variable",
		zap.String("name", name))

	// s.log.Info("ctx values", zap.Any("ctx", ctx))

	pid, found := pubsub.GetPID(ctx)
	if !found {
		s.log.Error("pubsub context not found")
	}

	ns := pid.ID.NS

	nameID := registry.ParseID(name)
	fullNameID := nameID.WithDefaultNS(ns)

	valueStored, found := s.values.Load(fullNameID)
	if !found {
		s.log.Error("variable not found", zap.String("name", name))
		return "", env.ErrVariableNotFound
	}

	value, ok := valueStored.(EnvValue)
	if !ok {
		s.log.Error("invalid variable payload", zap.String("name", name))
		return "", env.ErrVariableNotFound
	}

	s.log.Info("value stored", zap.Any("value", value))

	returnValue := value.Value
	if returnValue == "" {
		returnValue = value.DefaultValue
	}

	return returnValue, nil

	// var valueDeclaration env.Variable
	// var found bool
	//
	//s.variables.Range(func(key, value interface{}) bool {
	//	declaration := value.(env.Variable)
	//	if declaration.Name == name {
	//		valueDeclaration = declaration
	//		found = true
	//		return false // Stop iteration once found
	//	}
	//	return true // Continue iteration if not found
	//})
	//
	//s.log.Debug("variable found", zap.String("name", name))
	//
	//if !found {
	//	s.log.Error("variable not found", zap.String("name", name))
	//	return "", env.ErrVariableNotFound
	//}
	//
	//storedStorage, found := s.storages.Load(valueDeclaration.StorageID)
	//
	//if !found {
	//	s.log.Error("storage not found", zap.String("storage", valueDeclaration.StorageID))
	//	return "", fmt.Errorf("storage %s not found", valueDeclaration.StorageID)
	//}
	//
	//storage, ok := storedStorage.(env.Storage)
	//if !ok {
	//	s.log.Error("invalid storage type", zap.String("storage", valueDeclaration.StorageID))
	//	return "", fmt.Errorf("invalid storage type for %s", valueDeclaration.StorageID)
	//}
	//
	//value, err := storage.Get(ctx, valueDeclaration.EnvName)
	//if err != nil {
	//	s.log.Error("failed to get variable", zap.String("name", name), zap.Error(err))
	//	return "", fmt.Errorf("failed to get variable %s from storage %s: %w",
	//		valueDeclaration.EnvName, valueDeclaration.StorageID, err)
	//}
	//
	//return value, nil
}
