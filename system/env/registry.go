package env

import (
	"context"
	"errors"
	"fmt"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type Registry struct {
	ctx             context.Context
	log             *zap.Logger
	bus             event.Bus
	storages        sync.Map // map[registry.ID]env.Storage
	variablesByID   sync.Map // map[registry.ID]env.Variable
	variablesByName sync.Map // map[string]registry.ID - name -> ID mapping
	subscriber      *eventbus.Subscriber
}

func NewRegistry(bus event.Bus, log *zap.Logger) *Registry {
	return &Registry{
		log: log,
		bus: bus,
	}
}

func (r *Registry) Start(ctx context.Context) error {
	r.ctx = ctx
	subscriber, err := eventbus.NewSubscriber(ctx, r.bus, env.System, "(storage|variable).*", r.handleEvent)
	if err != nil {
		return env.NewSubscriberError(err)
	}
	r.subscriber = subscriber
	return nil
}

func (r *Registry) Stop() error {
	if r.subscriber != nil {
		r.bus.Unsubscribe(r.ctx, r.subscriber.ID())
	}
	return nil
}

func (r *Registry) getEnvName(variable *env.Variable) string {
	if variable.Name != "" {
		return variable.Name
	}
	return variable.ID.String()
}

// getCurrentNamespaceFromContext returns the current namespace from the provided context
func (r *Registry) getCurrentNamespaceFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}

	// Try to get namespace from FrameContext
	cc := ctxapi.FrameFromContext(ctx)
	if cc != nil {
		if idValue, ok := cc.Get(runtime.FrameIDKey); ok {
			if callID, ok := idValue.(registry.ID); ok {
				return callID.NS
			}
		}
	}

	return ""
}

func (r *Registry) handleEvent(e event.Event) {
	switch e.Kind {
	case env.StorageRegister:
		r.registerStorage(e)
	case env.StorageDelete:
		r.deleteStorage(e)
	case env.VariableRegister:
		r.registerVariable(e)
	case env.VariableUpdate:
		r.updateVariable(e)
	case env.VariableDelete:
		r.deleteVariable(e)
	case registry.Accept, registry.Reject:
	default:
		r.log.Warn("unknown event kind", zap.String("kind", e.Kind), zap.String("path", e.Path))
	}
}

func (r *Registry) registerStorage(e event.Event) {
	storage, ok := e.Data.(env.Storage)
	if !ok || storage == nil {
		r.log.Error("invalid storage payload", zap.String("path", e.Path))
		r.sendReject(e.Path, "invalid storage data type")
		return
	}

	storageID := registry.ParseID(e.Path)
	r.storages.Store(storageID, storage)
	r.sendAccept(e.Path)

	r.log.Debug("storage registered", zap.String("id", storageID.String()))
}

// RegisterStorage registers a storage backend directly (synchronous).
func (r *Registry) RegisterStorage(id registry.ID, storage env.Storage) {
	r.storages.Store(id, storage)
	r.log.Debug("storage registered directly", zap.String("id", id.String()))
}

func (r *Registry) deleteStorage(e event.Event) {
	storageID := registry.ParseID(e.Path)
	r.storages.Delete(storageID)
	r.sendAccept(e.Path)
	r.log.Debug("storage deleted", zap.String("id", storageID.String()))
}

func (r *Registry) registerVariable(e event.Event) {
	variable, ok := e.Data.(env.Variable)
	if !ok {
		r.log.Error("invalid variable payload", zap.String("path", e.Path))
		r.sendReject(e.Path, "invalid variable data type")
		return
	}

	if err := variable.Validate(); err != nil {
		r.log.Error("invalid variable", zap.String("path", e.Path), zap.Error(err))
		r.sendReject(e.Path, env.NewInvalidVariableError(err).Error())
		return
	}

	if _, exists := r.storages.Load(variable.StorageID); !exists {
		r.log.Error("referenced storage not found", zap.String("path", e.Path), zap.String("storage_id", variable.StorageID.String()))
		r.sendReject(e.Path, "referenced storage not found")
		return
	}

	envName := r.getEnvName(&variable)

	if _, exists := r.variablesByName.Load(envName); exists {
		r.log.Error("variable name already exists", zap.String("path", e.Path), zap.String("base_name", envName))
		r.sendReject(e.Path, env.NewVariableNameExistsError(envName).Error())
		return
	}

	r.variablesByID.Store(variable.ID, variable)
	r.variablesByName.Store(envName, variable.ID)
	r.sendAccept(e.Path)
	r.log.Debug("variable registered", zap.String("id", variable.ID.String()), zap.String("name", variable.Name), zap.String("base_name", envName))
}

func (r *Registry) updateVariable(e event.Event) {
	variable, ok := e.Data.(env.Variable)
	if !ok {
		r.log.Error("invalid variable payload", zap.String("path", e.Path))
		r.sendReject(e.Path, "invalid variable data type")
		return
	}

	if err := variable.Validate(); err != nil {
		r.log.Error("invalid variable", zap.String("path", e.Path), zap.Error(err))
		r.sendReject(e.Path, env.NewInvalidVariableError(err).Error())
		return
	}

	if _, exists := r.storages.Load(variable.StorageID); !exists {
		r.log.Error("referenced storage not found", zap.String("path", e.Path), zap.String("storage_id", variable.StorageID.String()))
		r.sendReject(e.Path, "referenced storage not found")
		return
	}

	envName := r.getEnvName(&variable)

	if existingID, exists := r.variablesByName.Load(envName); exists {
		if existingVarID, ok := existingID.(registry.ID); ok && !existingVarID.Equal(variable.ID) {
			r.log.Error("variable name already exists", zap.String("path", e.Path), zap.String("base_name", envName))
			r.sendReject(e.Path, env.NewVariableNameExistsError(envName).Error())
			return
		}
	}

	// Clean up old name mappings if variable exists
	if storedVar, exists := r.variablesByID.Load(variable.ID); exists {
		if oldVariable, ok := storedVar.(env.Variable); ok {
			oldBaseName := r.getEnvName(&oldVariable)
			if oldBaseName != envName {
				r.variablesByName.Delete(oldBaseName)
			}
		}
	}

	r.variablesByID.Store(variable.ID, variable)
	r.variablesByName.Store(envName, variable.ID)

	r.sendAccept(e.Path)

	r.log.Debug("variable updated", zap.String("id", variable.ID.String()), zap.String("name", variable.Name), zap.String("base_name", envName))
}

func (r *Registry) deleteVariable(e event.Event) {
	varID := registry.ParseID(e.Path)

	if storedVar, exists := r.variablesByID.Load(varID); exists {
		if variable, ok := storedVar.(env.Variable); ok {
			envName := r.getEnvName(&variable)
			r.variablesByName.Delete(envName)
		}
	}

	r.variablesByID.Delete(varID)
	r.sendAccept(e.Path)
	r.log.Debug("variable deleted", zap.String("id", varID.String()))
}

func (r *Registry) findVariableByID(id registry.ID) (*env.Variable, error) {
	if stored, exists := r.variablesByID.Load(id); exists {
		if variable, ok := stored.(env.Variable); ok {
			return &variable, nil
		}
	}
	return nil, env.ErrVariableNotFound
}

func (r *Registry) findVariable(ctx context.Context, name string) (*env.Variable, error) {
	// First try to find by exact name
	if storedID, exists := r.variablesByName.Load(name); exists {
		if varID, ok := storedID.(registry.ID); ok {
			return r.findVariableByID(varID)
		}
	}

	// Parse the name as an ID
	nameID := registry.ParseID(name)

	// If no namespace provided, try to add current namespace from context
	if nameID.NS == "" {
		currentNS := r.getCurrentNamespaceFromContext(ctx)
		if currentNS != "" {
			// Try with current namespace
			fullNameID := nameID.WithDefaultNS(currentNS)
			r.log.Debug("trying with current namespace", zap.String("original_name", name), zap.String("full_name", fullNameID.String()))

			// Try to find directly by ID in variablesByID
			if variable, err := r.findVariableByID(fullNameID); err == nil {
				r.log.Debug("found variable with current namespace", zap.String("search_name", name), zap.String("found_id", fullNameID.String()))
				return variable, nil
			}
		}
	}

	return r.findVariableByID(nameID)
}

// GetStorage retrieves storage by ID from the registry's in-memory map.
// Context is accepted for interface consistency but not used since storage
// lookup is a local map operation.
func (r *Registry) GetStorage(_ context.Context, id registry.ID) (env.Storage, error) {
	stored, exists := r.storages.Load(id)
	if !exists {
		return nil, env.ErrStorageNotFound
	}
	storage, ok := stored.(env.Storage)
	if !ok {
		return nil, env.NewInvalidStorageTypeError(id.String())
	}
	return storage, nil
}

func (r *Registry) Get(ctx context.Context, name string) (string, error) {
	variable, err := r.findVariable(ctx, name)
	if err != nil {
		return "", err
	}
	return r.getValue(ctx, variable)
}

func (r *Registry) Lookup(ctx context.Context, name string) (string, bool, error) {
	variable, err := r.findVariable(ctx, name)
	if err != nil {
		return "", false, err
	}
	return r.lookupValue(ctx, variable)
}

func (r *Registry) Set(ctx context.Context, name string, value string) error {
	variable, err := r.findVariable(ctx, name)
	if err != nil {
		return err
	}
	return r.setValue(ctx, variable, value)
}

func (r *Registry) getValue(ctx context.Context, variable *env.Variable) (string, error) {
	value, found, err := r.lookupValue(ctx, variable)
	if err != nil {
		return "", err
	}
	if !found {
		// Return default if set, otherwise return not found error
		if variable.DefaultValue != "" {
			return variable.DefaultValue, nil
		}
		return "", env.ErrVariableNotFound
	}
	return value, nil
}

func (r *Registry) lookupValue(ctx context.Context, variable *env.Variable) (string, bool, error) {
	storage, err := r.GetStorage(ctx, variable.StorageID)
	if err != nil {
		return "", false, err
	}

	envName := r.getEnvName(variable)
	value, err := storage.Get(ctx, envName)
	if err != nil {
		if errors.Is(err, env.ErrVariableNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	// Empty string is treated as not set
	if value == "" {
		return "", false, nil
	}
	return value, true, nil
}

func (r *Registry) setValue(ctx context.Context, variable *env.Variable, value string) error {
	if variable.ReadOnly {
		return env.ErrVariableReadOnly
	}

	storage, err := r.GetStorage(ctx, variable.StorageID)
	if err != nil {
		return err
	}

	envName := r.getEnvName(variable)
	return storage.Set(ctx, envName, value)
}

func (r *Registry) All(ctx context.Context) (map[string]string, error) {
	result := make(map[string]string)

	r.storages.Range(func(key, value interface{}) bool {
		storage, ok := value.(env.Storage)
		if !ok {
			return true
		}

		variables, err := storage.List(ctx)
		if err != nil {
			r.log.Error("failed to list variables from storage", zap.String("storage_id", fmt.Sprintf("%v", key)), zap.Error(err))
			return true
		}

		for name, val := range variables {
			result[name] = val
		}
		return true
	})

	return result, nil
}

func (r *Registry) sendAccept(path event.Path) {
	r.bus.Send(r.ctx, event.Event{
		System: env.System,
		Kind:   env.Accepted,
		Path:   path,
	})
}

func (r *Registry) sendReject(path event.Path, reason string) {
	r.bus.Send(r.ctx, event.Event{
		System: env.System,
		Kind:   env.Rejected,
		Path:   path,
		Data:   reason,
	})
}

// Compile-time interface check
var _ env.Registry = (*Registry)(nil)
