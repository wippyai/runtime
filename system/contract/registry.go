package contract

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// bindingWithMeta wraps a binding with its ID and metadata for runtime use
type bindingWithMeta struct {
	ID      registry.ID
	Meta    attrs.Bag
	Binding *contract.Binding
}

// Registry implements contract.Registry interface for data management
type Registry struct {
	ctx    context.Context
	bus    event.Bus
	logger *zap.Logger

	definitions     map[string]*contract.Definition
	bindings        map[string]*bindingWithMeta
	defaultBindings map[string]registry.ID // contractID -> bindingID
	mu              sync.RWMutex

	subscriber *eventbus.Subscriber
}

// NewContractRegistry creates a new contract registry
func NewContractRegistry(bus event.Bus, logger *zap.Logger) *Registry {
	return &Registry{
		bus:             bus,
		logger:          logger,
		definitions:     make(map[string]*contract.Definition),
		bindings:        make(map[string]*bindingWithMeta),
		defaultBindings: make(map[string]registry.ID),
	}
}

// Start begins listening for contract events
func (r *Registry) Start(ctx context.Context) error {
	r.ctx = ctx

	sub, err := eventbus.NewSubscriber(
		ctx,
		r.bus,
		contract.System,
		"contract.(definition|binding).(register|update|delete)",
		r.handleEvent,
	)
	if err != nil {
		return NewSubscriberError(err)
	}
	r.subscriber = sub

	return nil
}

// Stop cleanly shuts down the registry
func (r *Registry) Stop() error {
	if r.subscriber != nil {
		r.subscriber.Close()
	}
	return nil
}

func (r *Registry) handleEvent(e event.Event) {
	switch e.Kind {
	case contract.RegisterDefinition:
		r.registerDefinition(e)
	case contract.UpdateDefinition:
		r.updateDefinition(e)
	case contract.DeleteDefinition:
		r.deleteDefinition(e)
	case contract.RegisterBinding:
		r.registerBinding(e)
	case contract.UpdateBinding:
		r.updateBinding(e)
	case contract.DeleteBinding:
		r.deleteBinding(e)
	default:
		r.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (r *Registry) registerDefinition(e event.Event) {
	def, ok := e.Data.(*contract.Definition)
	if !ok {
		r.logger.Error("invalid definition payload",
			zap.String("path", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		r.sendReject(e.Path, "invalid definition payload")
		return
	}

	// Populate runtime ID and metadata if not set
	defID := registry.ParseID(e.Path)
	def.ID = defID
	if def.Meta == nil {
		def.Meta = make(attrs.Bag)
	}

	r.mu.Lock()
	r.definitions[e.Path] = def
	r.mu.Unlock()

	r.logger.Debug("contract definition registered", zap.String("id", e.Path))
	r.sendAccept(e.Path)
}

func (r *Registry) updateDefinition(e event.Event) {
	def, ok := e.Data.(*contract.Definition)
	if !ok {
		r.sendReject(e.Path, "invalid definition payload")
		return
	}

	// Populate runtime ID and metadata if not set
	defID := registry.ParseID(e.Path)
	def.ID = defID
	if def.Meta == nil {
		def.Meta = make(attrs.Bag)
	}

	r.mu.Lock()
	r.definitions[e.Path] = def
	r.mu.Unlock()

	r.logger.Debug("contract definition updated", zap.String("id", e.Path))
	r.sendAccept(e.Path)
}

func (r *Registry) deleteDefinition(e event.Event) {
	r.mu.Lock()
	delete(r.definitions, e.Path)
	// Clean up any default bindings for this contract
	delete(r.defaultBindings, e.Path)
	r.mu.Unlock()

	r.logger.Debug("contract definition deleted", zap.String("id", e.Path))
	r.sendAccept(e.Path)
}

func (r *Registry) registerBinding(e event.Event) {
	binding, ok := e.Data.(*contract.Binding)
	if !ok {
		r.logger.Error("invalid binding payload",
			zap.String("path", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		r.sendReject(e.Path, "invalid binding payload")
		return
	}

	bindingID := registry.ParseID(e.Path)

	// Populate runtime fields
	binding.ID = bindingID
	if binding.Meta == nil {
		binding.Meta = make(attrs.Bag)
	}

	r.mu.Lock()
	r.bindings[e.Path] = &bindingWithMeta{
		ID:      bindingID,
		Meta:    binding.Meta,
		Binding: binding,
	}

	// Update default bindings map
	r.updateDefaultBindings(binding, bindingID)
	r.mu.Unlock()

	r.logger.Debug("contract binding registered", zap.String("id", e.Path))
	r.sendAccept(e.Path)
}

func (r *Registry) updateBinding(e event.Event) {
	binding, ok := e.Data.(*contract.Binding)
	if !ok {
		r.sendReject(e.Path, "invalid binding payload")
		return
	}

	bindingID := registry.ParseID(e.Path)

	// Populate runtime fields
	binding.ID = bindingID
	if binding.Meta == nil {
		binding.Meta = make(attrs.Bag)
	}

	r.mu.Lock()
	// Remove old default bindings for this binding ID
	r.removeDefaultBindings(bindingID)

	r.bindings[e.Path] = &bindingWithMeta{
		ID:      bindingID,
		Meta:    binding.Meta,
		Binding: binding,
	}

	// Update default bindings map with new defaults
	r.updateDefaultBindings(binding, bindingID)
	r.mu.Unlock()

	r.logger.Debug("contract binding updated", zap.String("id", e.Path))
	r.sendAccept(e.Path)
}

func (r *Registry) deleteBinding(e event.Event) {
	bindingID := registry.ParseID(e.Path)

	r.mu.Lock()
	// Remove default bindings for this binding ID
	r.removeDefaultBindings(bindingID)
	delete(r.bindings, e.Path)
	r.mu.Unlock()

	r.logger.Debug("contract binding deleted", zap.String("id", e.Path))
	r.sendAccept(e.Path)
}

// updateDefaultBindings updates the default bindings map for contracts that are marked as default
// Assumes r.mu is already locked
func (r *Registry) updateDefaultBindings(binding *contract.Binding, bindingID registry.ID) {
	for _, bc := range binding.Contracts {
		if bc.Default {
			r.defaultBindings[bc.Contract.String()] = bindingID
		}
	}
}

// removeDefaultBindings removes any default bindings for the given binding ID
// Assumes r.mu is already locked
func (r *Registry) removeDefaultBindings(bindingID registry.ID) {
	for contractID, defaultBindingID := range r.defaultBindings {
		if defaultBindingID == bindingID {
			delete(r.defaultBindings, contractID)
		}
	}
}

func (r *Registry) sendAccept(path event.Path) {
	r.bus.Send(r.ctx, event.Event{
		System: contract.System,
		Kind:   contract.Accept,
		Path:   path,
	})
}

func (r *Registry) sendReject(path event.Path, reason string) {
	r.bus.Send(r.ctx, event.Event{
		System: contract.System,
		Kind:   contract.Reject,
		Path:   path,
		Data:   reason,
	})
}

// GetContract implements contract.Registry interface
func (r *Registry) GetContract(_ context.Context, id registry.ID) (contract.Contract, error) {
	r.mu.RLock()
	def, exists := r.definitions[id.String()]
	r.mu.RUnlock()

	if !exists {
		return nil, contract.NewContractNotFoundError(id)
	}

	return &contractImpl{
		id:  id,
		def: def,
	}, nil
}

// GetBinding implements contract.Registry interface
func (r *Registry) GetBinding(_ context.Context, id registry.ID) (*contract.Binding, error) {
	r.mu.RLock()
	bindingWithMeta, exists := r.bindings[id.String()]
	r.mu.RUnlock()

	if !exists {
		return nil, contract.NewBindingNotFoundError(id)
	}

	return bindingWithMeta.Binding, nil
}

// GetBindingsForContract implements contract.Registry interface
func (r *Registry) GetBindingsForContract(_ context.Context, contractID registry.ID) ([]registry.ID, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var bindingIDs []registry.ID
	for _, bindingWithMeta := range r.bindings {
		for _, boundContract := range bindingWithMeta.Binding.Contracts {
			if boundContract.Contract == contractID {
				bindingIDs = append(bindingIDs, bindingWithMeta.ID)
				break
			}
		}
	}

	return bindingIDs, nil
}

// GetDefaultBinding implements contract.Registry interface
func (r *Registry) GetDefaultBinding(_ context.Context, contractID registry.ID) (registry.ID, error) {
	r.mu.RLock()
	bindingID, exists := r.defaultBindings[contractID.String()]
	r.mu.RUnlock()

	if !exists {
		return registry.NewID("", ""), contract.NewNoDefaultBindingError(contractID)
	}

	return bindingID, nil
}

// contractImpl implements contract.Contract interface
type contractImpl struct {
	id  registry.ID
	def *contract.Definition
}

func (c *contractImpl) ID() registry.ID {
	return c.id
}

func (c *contractImpl) Meta() attrs.Bag {
	return c.def.Meta
}

func (c *contractImpl) Methods() []contract.MethodDef {
	return c.def.Methods
}

func (c *contractImpl) Method(name string) (*contract.MethodDef, error) {
	for _, method := range c.def.Methods {
		if method.Name == name {
			return &method, nil
		}
	}
	return nil, contract.NewMethodNotFoundError(name, c.id)
}

// Ensure Registry implements contract.Registry interface
var _ contract.Registry = (*Registry)(nil)
