package env

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	serviceenv "github.com/ponyruntime/pony/service/env"
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

	storageID := registry.ParseID(e.Path)
	s.storages.Store(storageID, storage)
	s.sendAccept(e.Path)
}

func (s *Registry) registerVariable(e event.Event) {
	s.log.Debug("registering variable",
		zap.String("path", e.Path),
		zap.String("kind", e.Kind))

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
		// Check if the new storage is a router storage that includes the existing storage
		storageID := registry.ParseID(variable.StorageID)
		storedStorage, found := s.storages.Load(storageID)
		if found {
			if storage, ok := storedStorage.(env.Storage); ok && serviceenv.IsRouterStorage(storage) {
				s.log.Debug("allowing router storage variable to coexist with underlying storage",
					zap.String("env_name", variable.EnvName),
					zap.String("existing_storage", storedEnvValue.StorageID),
					zap.String("new_storage", variable.StorageID))
			} else {
				s.log.Debug("variable already exists in different storage",
					zap.String("env_name", variable.EnvName),
					zap.String("existing_storage", storedEnvValue.StorageID),
					zap.String("new_storage", variable.StorageID))
				s.sendReject(e.Path, fmt.Sprintf("variable with the name %s already stored in a different storage", variable.EnvName))
				return
			}
		} else {
			s.log.Debug("storage not found during variable registration check",
				zap.String("storage_id", storageID.String()))
			s.sendReject(e.Path, "storage not found during variable registration")
			return
		}
	}
	s.log.Debug("no existing variable declaration found or same storage",
		zap.String("env_name", variable.EnvName))

	// Store variable by its name
	s.variables.Store(variable.Name, variable)

	storedStorage, found := s.storages.Load(storageID)
	if !found {
		s.log.Error("storage not found",
			zap.String("storage", storageID.String()),
			zap.String("variable", variableName))
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
		// If the variable has a default value, allow registration with empty value
		if variable.DefaultValue != "" {
			s.log.Info("variable not found in storage but has default value, allowing registration",
				zap.String("variable", variableName),
				zap.String("default_value", variable.DefaultValue))
			variableValue = ""
		} else {
			s.log.Error("failed to get variable value",
				zap.String("variable", variableName),
				zap.Error(err))
			s.sendReject(e.Path, "variable not found in storage")
			return
		}
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
	s.log.Info("variable registered successfully",
		zap.String("path", e.Path),
		zap.String("name", variable.Name),
		zap.String("env_name", variable.EnvName),
		zap.String("default_value", variable.DefaultValue))
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

	storedStorage, found := s.storages.Load(storageID)
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
	s.log.Info("getEnvValue called",
		zap.String("name", name))

	// First try to get value by name
	value, err := s.getEnvValueByName(ctx, name)
	if err == nil {
		s.log.Info("variable found by name",
			zap.String("name", name))
		return value, nil
	}

	s.log.Info("variable not found by name, trying by env name",
		zap.String("name", name))

	// If not found by name, try to get by env name
	value, err = s.getEnvValueByEnvName(ctx, name)
	if err == nil {
		s.log.Info("variable found by env name",
			zap.String("name", name))
		return value, nil
	}

	s.log.Info("variable not found by either method",
		zap.String("name", name))
	return nil, env.ErrVariableNotFound
}

func (s *Registry) getEnvValueByName(ctx context.Context, name string) (*EnvValue, error) {
	nameID := registry.ParseID(name)

	s.log.Debug("getEnvValueByName. getting env value", zap.String("name", name))

	ns := nameID.NS
	if ns == "" {
		pid, pidFound := pubsub.GetPID(ctx)
		// s.log.Debug("getEnvValueByName. getting env value", zap.String("name", name), zap.Any("pid", pid), zap.Any("pidFound", pidFound))

		if !pidFound {
			return nil, env.ErrVariableNotFound
		}

		ns = pid.ID.NS
	}
	fullNameID := nameID.WithDefaultNS(ns)
	s.log.Debug("getEnvValueByName. getting env value", zap.String("name", name), zap.Any("fullName", fullNameID))

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
	var routerVariable EnvValue
	var routerFound bool

	// todo: this is hot path btw, need secondary map
	s.values.Range(func(_ interface{}, value interface{}) bool {
		v := value.(EnvValue)
		if v.EnvName == envName {
			// Check if this variable uses a router storage
			storageID := registry.ParseID(v.StorageID)
			storedStorage, storageFound := s.storages.Load(storageID)
			if storageFound {
				if storage, ok := storedStorage.(env.Storage); ok && serviceenv.IsRouterStorage(storage) {
					// Prioritize router storage variables over regular storage variables
					routerVariable = v
					routerFound = true
					return false // Found router variable, stop searching
				}

				// Only store the first non-router variable as fallback
				variable = v
				found = true
			} else {
				// If storage not found, treat as regular variable
				variable = v
				found = true
			}
		}
		return true
	})

	// Return router variable if found, otherwise return the first found variable
	if routerFound {
		s.log.Debug("found router variable by env name",
			zap.String("env_name", envName),
			zap.String("storage_id", routerVariable.StorageID))
		return &routerVariable, nil
	}

	if found {
		s.log.Debug("found regular variable by env name",
			zap.String("env_name", envName),
			zap.String("storage_id", variable.StorageID))
		return &variable, nil
	}

	s.log.Warn("variable not found", zap.String("name", envName))
	return nil, env.ErrVariableNotFound
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

func (s *Registry) GetFromStorage(ctx context.Context, name string) (string, error) {
	// Find the last colon to separate storage ID from environment variable name
	lastColonIndex := strings.LastIndex(name, ":")
	if lastColonIndex == -1 {
		return "", fmt.Errorf("invalid name format: expected 'storage:envvar' but got '%s'", name)
	}

	storageIDStr := name[:lastColonIndex]
	envVarName := name[lastColonIndex+1:]

	if envVarName == "" {
		return "", fmt.Errorf("invalid name format: environment variable name cannot be empty")
	}

	// if storage ID is empty, iterate over all storages
	if storageIDStr == "" {
		return s.getFromAnyStorage(ctx, envVarName)
	}

	// get from specific storage
	return s.getFromSpecificStorage(ctx, storageIDStr, envVarName, name)
}

func (s *Registry) getFromAnyStorage(ctx context.Context, envVarName string) (string, error) {
	s.log.Debug("getFromAnyStorage called")

	// Iterate through all storages to find the variable
	var foundVariable string
	var foundStorageKey interface{}

	s.storages.Range(func(key interface{}, value interface{}) bool {
		storage, ok := value.(env.Storage)
		if !ok {
			s.log.Error("invalid storage type",
				zap.String("storage", fmt.Sprintf("%v", key)),
				zap.String("type", fmt.Sprintf("%T", value)))
			return true // continue with other storages
		}

		// Try to get the variable from this storage
		variable, err := storage.Get(ctx, envVarName)
		if err == nil {
			foundVariable = variable
			foundStorageKey = key
			return false // stop iteration
		}

		return true // continue with other storages
	})

	if foundVariable != "" {
		s.log.Info("got variable from storage",
			zap.String("storage", fmt.Sprintf("%v", foundStorageKey)),
			zap.String("variable", envVarName),
			zap.String("value", foundVariable))
		return foundVariable, nil
	}

	// If we reach here, the variable was not found in any storage
	s.log.Error("variable not found in any storage", zap.String("variable", envVarName))
	return "", env.ErrVariableNotFound
}

func (s *Registry) getFromSpecificStorage(ctx context.Context, storageIDStr, envVarName, originalName string) (string, error) {
	// Parse the storage ID
	storageID := registry.ParseID(storageIDStr)

	// Look up the specific storage
	storedStorage, found := s.storages.Load(storageID)
	if !found {
		s.log.Error("storage not found",
			zap.String("storage", storageID.String()),
			zap.String("name", originalName),
		)
		return "", env.ErrVariableNotFound
	}

	storage, ok := storedStorage.(env.Storage)
	if !ok {
		s.log.Error("invalid storage type",
			zap.String("storage", storageID.String()),
			zap.String("type", fmt.Sprintf("%T", storedStorage)))
		return "", env.ErrVariableNotFound
	}

	// Get the environment variable from the specific storage
	variable, err := storage.Get(ctx, envVarName)
	if err != nil {
		s.log.Error("failed to get variable from storage",
			zap.String("storage", storageID.String()),
			zap.String("variable", envVarName),
			zap.Error(err))
		return "", err
	}

	return variable, nil
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
	storageID := registry.ParseID(envValue.StorageID)
	storedStorage, found := s.storages.Load(storageID)
	if !found {
		s.log.Error("storage not found", zap.String("storage", storageID.String()))
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
