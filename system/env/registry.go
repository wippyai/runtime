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
	EnvName      string
	Value        string
	DefaultValue string
	ReadOnly     bool
}

type Registry struct {
	ctx        context.Context
	log        *zap.Logger
	bus        event.Bus
	storages   sync.Map // map[event.Path]env.Storage (storage)
	variables  sync.Map // map[name]env.Variable (declaration)
	values     sync.Map // map[registry.ID]EnvValue (value)
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
	subscriber, err := eventbus.NewSubscriber(ctx, s.bus, env.System, "(storage|variable).*", s.handleEvent)
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
	case env.VariableRegister:
		s.registerVariable(e)
	case env.VariableUpdate:
		s.updateVariable(e)
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
			zap.String("type", fmt.Sprintf("%T", e.Data)),
			zap.Any("data", e.Data))
		s.sendReject(e.Path, "invalid storage data type")
		return
	}

	s.storages.Store(e.Path, storage)
	s.sendAccept(e.Path)
}

func (s *Registry) registerVariable(e event.Event) {
	variable, ok := e.Data.(env.Variable)
	if !ok {
		s.log.Error("invalid variable payload",
			zap.String("type", fmt.Sprintf("%T", e.Data)),
			zap.Any("data", e.Data))
		s.sendReject(e.Path, "invalid variable data type")
		return
	}

	variableID := registry.ParseID(e.Path)
	storageID := registry.ParseID(variable.StorageID)
	variableName := variable.EnvName

	// Only reject if the variable exists in a different storage
	storedEnvValue, _ := s.getEnvDeclarationByEnvName(s.ctx, variable.EnvName)
	if storedEnvValue != nil && storedEnvValue.StorageID != variable.StorageID {
		s.sendReject(e.Path, fmt.Sprintf("variable with the name %s already stored in a different storage", variable.EnvName))
		return
	}

	// Store variable by its name
	s.variables.Store(variable.Name, variable)

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

	//
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
		EnvName:      variable.EnvName,
		Value:        variableValue,
		DefaultValue: variable.DefaultValue,
		ReadOnly:     variable.ReadOnly,
	}

	s.values.Store(variableID, value)
	s.sendAccept(e.Path)
}

func (s *Registry) updateVariable(e event.Event) {
	variable, ok := e.Data.(env.Variable)
	if !ok {
		s.log.Error("invalid variable payload",
			zap.String("type", fmt.Sprintf("%T", e.Data)),
			zap.Any("data", e.Data))
		s.sendReject(e.Path, "invalid variable data type")
		return
	}

	variableID := registry.ParseID(e.Path)
	storageID := registry.ParseID(variable.StorageID)
	variableName := variable.EnvName

	// Overwrite variable by its name
	s.variables.Store(variable.Name, variable)

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

	//
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
		EnvName:      variable.EnvName,
		Value:        variableValue,
		DefaultValue: variable.DefaultValue,
		ReadOnly:     variable.ReadOnly,
	}

	s.values.Store(variableID, value)
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

// All returns all available variables from all storages
func (s *Registry) All(ctx context.Context) (map[string]string, error) {
	result := make(map[string]string)

	// Iterate through all storages
	s.storages.Range(func(_ interface{}, value interface{}) bool {
		if storage, ok := value.(env.Storage); ok {
			// Get all variables from this storage
			variables, err := storage.List(ctx)
			if err != nil {
				s.log.Error("failed to list variables from storage", zap.Error(err))
				return true // continue with other storages
			}

			// Add variables to result map
			for name, val := range variables {
				result[name] = val
			}
		}
		return true
	})

	return result, nil
}

func (s *Registry) getEnvValue(ctx context.Context, name string) (*EnvValue, error) {
	// First try to get value by name
	value, err := s.getEnvValueByName(ctx, name)
	if err == nil {
		return value, nil
	}

	// If not found by name, try to get by env name
	value, err = s.getEnvValueByEnvName(ctx, name)
	if err == nil {
		return value, nil
	}

	return nil, env.ErrVariableNotFound
}

func (s *Registry) getEnvValueByName(ctx context.Context, name string) (*EnvValue, error) {
	nameID := registry.ParseID(name)

	ns := nameID.NS
	if ns == "" {
		pid, pidFound := pubsub.GetPID(ctx)
		if !pidFound {
			s.log.Error("PID not found")
			return nil, env.ErrVariableNotFound
		}

		ns = pid.ID.NS
	}
	fullNameID := nameID.WithDefaultNS(ns)

	// try to locate a value by id(ns:name)
	valueStored, valueFound := s.values.Load(fullNameID)
	if valueFound {
		value, ok := valueStored.(EnvValue)
		if !ok {
			s.log.Error("invalid variable payload", zap.String("name", name))
			return nil, env.ErrVariableNotFound
		}

		return &value, nil
	}

	return nil, env.ErrVariableNotFound
}

func (s *Registry) getEnvValueByEnvName(_ context.Context, envName string) (*EnvValue, error) {
	var variable EnvValue
	var found bool

	// todo: this is hot path btw, need secondary map
	s.values.Range(func(_ interface{}, value interface{}) bool {
		v := value.(EnvValue)
		if v.EnvName == envName {
			variable = v
			found = true
			return false
		}
		return true
	})

	if !found {
		s.log.Warn("variable not found", zap.String("name", envName))

		return nil, env.ErrVariableNotFound
	}

	return &variable, nil
}

func (s *Registry) getEnvDeclarationByEnvName(_ context.Context, envName string) (*env.Variable, error) {
	var variable env.Variable
	var found bool
	s.variables.Range(func(_ interface{}, value interface{}) bool {
		v := value.(env.Variable)
		if v.EnvName == envName {
			variable = v
			found = true
			return false
		}
		return true
	})

	if !found {
		return nil, env.ErrVariableNotFound
	}

	return &variable, nil
}

// Get retrieves an environment variable by name from a specific storage
func (s *Registry) Get(ctx context.Context, name string) (string, error) {
	value, err := s.getEnvValue(ctx, name)
	if err != nil {
		return "", err
	}

	if value.Value != "" {
		return value.Value, nil
	}

	return value.DefaultValue, nil
}

func (s *Registry) GetEventually(ctx context.Context, name string) (string, error) {
	// Subscribe to accept events first to avoid race conditions
	eventCh := make(chan event.Event, 1)
	subID, err := s.bus.SubscribeP(ctx, env.System, registry.Accept, eventCh)
	if err != nil {
		return "", fmt.Errorf("failed to subscribe to accept events: %w", err)
	}
	defer s.bus.Unsubscribe(ctx, subID)

	// Parse the name to get the expected ID
	nameID := registry.ParseID(name)
	ns := nameID.NS
	if ns == "" {
		pid, pidFound := pubsub.GetPID(ctx)
		if !pidFound {
			s.log.Error("PID not found")
			return "", env.ErrVariableNotFound
		}
		ns = pid.ID.NS
	}
	expectedID := nameID.WithDefaultNS(ns)

	// Now check if the value is already available
	value, err := s.getEnvValue(ctx, name)
	if err == nil {
		if value.Value != "" {
			return value.Value, nil
		}
		return value.DefaultValue, nil
	}

	// If not found, wait for it to become available
	return s.waitForVariableWithSubscription(ctx, name, expectedID, eventCh)
}

func (s *Registry) waitForVariableWithSubscription(ctx context.Context, name string, expectedID registry.ID, eventCh chan event.Event) (string, error) {
	s.log.Debug("waiting for variable to become available",
		zap.String("name", name),
		zap.String("expectedID", expectedID.String()))

	// Wait for accept events
	for {
		select {
		case evt := <-eventCh:
			// Check if this accept event is for our expected variable
			acceptedID := registry.ParseID(evt.Path)
			if acceptedID == expectedID {
				s.log.Debug("variable became available",
					zap.String("name", name),
					zap.String("acceptedID", acceptedID.String()))

				// Try to get the value again now that it's been accepted
				value, err := s.getEnvValue(ctx, name)
				if err != nil {
					return "", err
				}

				if value.Value != "" {
					return value.Value, nil
				}
				return value.DefaultValue, nil
			}
			// Continue waiting if this accept event is for a different variable

		case <-ctx.Done():
			return "", fmt.Errorf("context cancelled while waiting for variable %s: %w", name, ctx.Err())
		}
	}
}

func (s *Registry) Set(ctx context.Context, name string, value string) error {
	envValue, err := s.getEnvValue(ctx, name)
	if err != nil {
		return env.ErrVariableNotFound
	}

	if envValue.ReadOnly {
		s.log.Warn("variable is read-only", zap.String("name", name))
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

	err = storage.Set(ctx, envValue.EnvName, value)
	if err != nil {
		s.log.Error("failed to set variable", zap.String("name", name), zap.Error(err))
		return err
	}

	// Update the value in our cache
	envValue.Value = value
	s.values.Store(envValue.ID, *envValue)

	return nil
}
