package env

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

//nolint:revive // ok for now
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
	s.log.Info("handleEvent called",
		zap.String("system", e.System),
		zap.String("kind", e.Kind),
		zap.String("path", e.Path),
		zap.Any("data_type", fmt.Sprintf("%T", e.Data)),
		zap.Any("data", e.Data))

	switch e.Kind {
	case env.StorageRegister:
		s.log.Debug("processing storage register event",
			zap.String("path", e.Path),
			zap.Any("data_type", fmt.Sprintf("%T", e.Data)))
		s.registerStorage(e)
	case env.VariableRegister:
		s.log.Debug("processing variable register event",
			zap.String("path", e.Path),
			zap.Any("data_type", fmt.Sprintf("%T", e.Data)))
		s.registerVariable(e)
	case env.VariableUpdate:
		s.log.Debug("processing variable update event",
			zap.String("path", e.Path))
		s.updateVariable(e)
	case registry.Accept, registry.Reject:
		s.log.Debug("processing accept/reject event",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
		// nothing, self emitted
	default:
		s.log.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (s *Registry) registerStorage(e event.Event) {
	s.log.Debug("registerStorage called",
		zap.String("storage", e.Path),
		zap.Any("data_type", fmt.Sprintf("%T", e.Data)))

	storage, ok := e.Data.(env.Storage)
	if !ok {
		s.log.Error("invalid storage payload",
			zap.String("storage", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)),
			zap.Any("data", e.Data))
		s.sendReject(e.Path, "invalid storage data type")
		return
	}

	s.log.Debug("registering storage",
		zap.String("storage", e.Path),
		zap.Any("storage_type", fmt.Sprintf("%T", storage)))
	s.storages.Store(e.Path, storage)
	s.log.Debug("storage registered successfully",
		zap.String("storage", e.Path))
	s.sendAccept(e.Path)
}

func (s *Registry) registerVariable(e event.Event) {
	s.log.Debug("registerVariable called",
		zap.String("variable", e.Path),
		zap.Any("data_type", fmt.Sprintf("%T", e.Data)))

	variable, ok := e.Data.(env.Variable)
	if !ok {
		s.log.Error("invalid variable payload",
			zap.String("type", fmt.Sprintf("%T", e.Data)),
			zap.Any("data", e.Data))
		s.sendReject(e.Path, "invalid variable data type")
		return
	}

	s.log.Debug("processing variable registration",
		zap.String("name", variable.Name),
		zap.String("env_name", variable.EnvName),
		zap.String("storage_id", variable.StorageID))

	// Store variable by its name
	s.variables.Store(variable.Name, variable)
	s.log.Debug("variable stored in variables map",
		zap.String("name", variable.Name),
		zap.String("storage", variable.StorageID))

	variableID := registry.ParseID(e.Path)
	storageID := registry.ParseID(variable.StorageID)
	variableName := variable.EnvName

	s.log.Debug("looking up storage",
		zap.String("storage_id", storageID.String()))
	storedStorage, found := s.storages.Load(storageID.String())
	if !found {
		s.log.Error("storage not found",
			zap.String("storage", storageID.String()))
		s.sendReject(e.Path, "storage not found")
		return
	}

	storage, ok := storedStorage.(env.Storage)
	if !ok {
		s.log.Error("invalid storage type",
			zap.String("storage", storageID.String()),
			zap.String("type", fmt.Sprintf("%T", storedStorage)))
		s.sendReject(e.Path, "invalid storage type")
		return
	}

	variableValue, err := storage.Get(s.ctx, variableName)
	if err != nil {
		s.log.Error("failed to get variable value",
			zap.String("variable", variableName),
			zap.Error(err))
		s.sendReject(e.Path, "variable not found in storage")
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
	s.log.Info("variable value stored successfully",
		zap.Any("id", variableID),
		zap.Any("value", value))
	s.sendAccept(e.Path)
}

func (s *Registry) updateVariable(e event.Event) {
	// potentially leads to variable redefinition
	s.registerVariable(e)
}

func (s *Registry) sendAccept(path event.Path) {
	s.log.Info("sending accept event",
		zap.String("path", path))
	s.bus.Send(s.ctx, event.Event{
		System: env.System,
		Kind:   registry.Accept,
		Path:   path,
	})
}

func (s *Registry) sendReject(path event.Path, reason string) {
	s.log.Info("sending reject event",
		zap.String("path", path),
		zap.String("reason", reason))
	s.bus.Send(s.ctx, event.Event{
		System: env.System,
		Kind:   registry.Reject,
		Path:   path,
		Data:   reason,
	})
}

// All returns all env storages
func (s *Registry) All(_ context.Context) ([]env.Storage, error) {
	var storages []env.Storage
	s.storages.Range(func(_ interface{}, value interface{}) bool {
		if storage, ok := value.(env.Storage); ok {
			storages = append(storages, storage)
		}
		return true
	})
	return storages, nil
}

// Get retrieves an environment variable by name from a specific storage
func (s *Registry) Get(ctx context.Context, name string) (string, error) {
	s.log.Info("getting environment variable",
		zap.String("name", name))

	pid, found := pubsub.GetPID(ctx)
	if !found {
		s.log.Error("pubsub context not found")
		return "", env.ErrVariableNotFound
	}

	ns := pid.ID.NS
	nameID := registry.ParseID(name)
	fullNameID := nameID.WithDefaultNS(ns)

	// First try to get the value directly
	valueStored, found := s.values.Load(fullNameID)
	if !found {
		// If not found, try to find the variable by name
		var variable env.Variable
		var found bool
		s.variables.Range(func(_ interface{}, value interface{}) bool {
			v := value.(env.Variable)
			if v.Name == name {
				variable = v
				found = true
				return false
			}
			return true
		})

		if !found {
			s.log.Error("variable not found", zap.String("name", name))
			return "", env.ErrVariableNotFound
		}

		// Try to get the value from storage
		storedStorage, found := s.storages.Load(variable.StorageID)
		if !found {
			s.log.Error("storage not found", zap.String("storage", variable.StorageID))
			return "", env.ErrVariableNotFound
		}

		storage, ok := storedStorage.(env.Storage)
		if !ok {
			s.log.Error("invalid storage type", zap.String("storage", variable.StorageID))
			return "", env.ErrVariableNotFound
		}

		value, err := storage.Get(ctx, variable.EnvName)
		if err != nil {
			s.log.Error("failed to get variable", zap.String("name", name), zap.Error(err))
			return "", err
		}

		return value, nil
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
}

func (s *Registry) Set(ctx context.Context, name string, value string) error {
	s.log.Debug("setting variable", zap.String("name", name), zap.String("value", value))

	pid, found := pubsub.GetPID(ctx)
	if !found {
		s.log.Error("pubsub context not found")
		return env.ErrVariableNotFound
	}

	ns := pid.ID.NS
	nameID := registry.ParseID(name)
	fullNameID := nameID.WithDefaultNS(ns)

	// First try to get the value directly
	valueStored, found := s.values.Load(fullNameID)
	if !found {
		// If not found, try to find the variable by name
		var variable env.Variable
		var found bool
		s.variables.Range(func(_ interface{}, value interface{}) bool {
			v := value.(env.Variable)
			if v.Name == name {
				variable = v
				found = true
				return false
			}
			return true
		})

		if !found {
			s.log.Error("variable not found", zap.String("name", name))
			return env.ErrVariableNotFound
		}

		if variable.ReadOnly {
			s.log.Error("variable is read-only", zap.String("name", name))
			return env.ErrVariableReadOnly
		}

		// Try to get the storage
		storedStorage, found := s.storages.Load(variable.StorageID)
		if !found {
			s.log.Error("storage not found", zap.String("storage", variable.StorageID))
			return env.ErrVariableNotFound
		}

		storage, ok := storedStorage.(env.Storage)
		if !ok {
			s.log.Error("invalid storage type", zap.String("storage", variable.StorageID))
			return env.ErrVariableNotFound
		}

		err := storage.Set(ctx, variable.EnvName, value)
		if err != nil {
			s.log.Error("failed to set variable", zap.String("name", name), zap.Error(err))
			return err
		}

		// Update the value in our cache
		envValue := EnvValue{
			ID:           fullNameID,
			VariableID:   fullNameID.String(),
			StorageID:    variable.StorageID,
			Name:         variable.EnvName,
			Value:        value,
			DefaultValue: variable.DefaultValue,
			ReadOnly:     variable.ReadOnly,
		}
		s.values.Store(fullNameID, envValue)

		return nil
	}

	envValue, ok := valueStored.(EnvValue)
	if !ok {
		s.log.Error("invalid variable payload", zap.String("name", name))
		return env.ErrVariableNotFound
	}

	if envValue.ReadOnly {
		s.log.Error("variable is read-only", zap.String("name", name))
		return env.ErrVariableReadOnly
	}

	// Try to get the storage
	storedStorage, found := s.storages.Load(envValue.StorageID)
	if !found {
		s.log.Error("storage not found", zap.String("storage", envValue.StorageID))
		return env.ErrVariableNotFound
	}

	storage, ok := storedStorage.(env.Storage)
	if !ok {
		s.log.Error("invalid storage type", zap.String("storage", envValue.StorageID))
		return env.ErrVariableNotFound
	}

	err := storage.Set(ctx, envValue.Name, value)
	if err != nil {
		s.log.Error("failed to set variable", zap.String("name", name), zap.Error(err))
		return err
	}

	// Update the value in our cache
	envValue.Value = value
	s.values.Store(fullNameID, envValue)

	return nil
}
