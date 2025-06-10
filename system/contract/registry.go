package contract

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/contract"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

// bindingWithMeta wraps a binding with its ID and metadata for runtime use
type bindingWithMeta struct {
	ID      registry.ID
	Meta    registry.Metadata
	Binding *contract.Binding
}

// ContractRegistry implements contract.Registry interface for data management
type ContractRegistry struct {
	ctx    context.Context
	bus    event.Bus
	logger *zap.Logger

	definitions map[string]*contract.Definition
	bindings    map[string]*bindingWithMeta
	mu          sync.RWMutex

	subscriber *eventbus.Subscriber
}

// NewContractRegistry creates a new contract registry
func NewContractRegistry(bus event.Bus, logger *zap.Logger) *ContractRegistry {
	return &ContractRegistry{
		bus:         bus,
		logger:      logger,
		definitions: make(map[string]*contract.Definition),
		bindings:    make(map[string]*bindingWithMeta),
	}
}

// Start begins listening for contract events
func (r *ContractRegistry) Start(ctx context.Context) error {
	r.ctx = ctx

	sub, err := eventbus.NewSubscriber(
		ctx,
		r.bus,
		contract.System,
		"contract.(definition|binding).(register|update|delete)",
		r.handleEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	r.subscriber = sub

	return nil
}

// Stop cleanly shuts down the registry
func (r *ContractRegistry) Stop() error {
	if r.subscriber != nil {
		r.subscriber.Close()
	}
	return nil
}

func (r *ContractRegistry) handleEvent(e event.Event) {
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

func (r *ContractRegistry) registerDefinition(e event.Event) {
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
		def.Meta = make(registry.Metadata)
	}

	r.mu.Lock()
	r.definitions[e.Path] = def
	r.mu.Unlock()

	r.logger.Debug("contract definition registered", zap.String("id", e.Path))
	r.sendAccept(e.Path)
}

func (r *ContractRegistry) updateDefinition(e event.Event) {
	def, ok := e.Data.(*contract.Definition)
	if !ok {
		r.sendReject(e.Path, "invalid definition payload")
		return
	}

	// Populate runtime ID and metadata if not set
	defID := registry.ParseID(e.Path)
	def.ID = defID
	if def.Meta == nil {
		def.Meta = make(registry.Metadata)
	}

	r.mu.Lock()
	r.definitions[e.Path] = def
	r.mu.Unlock()

	r.logger.Debug("contract definition updated", zap.String("id", e.Path))
	r.sendAccept(e.Path)
}

func (r *ContractRegistry) deleteDefinition(e event.Event) {
	r.mu.Lock()
	delete(r.definitions, e.Path)
	r.mu.Unlock()

	r.logger.Debug("contract definition deleted", zap.String("id", e.Path))
	r.sendAccept(e.Path)
}

func (r *ContractRegistry) registerBinding(e event.Event) {
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
		binding.Meta = make(registry.Metadata)
	}

	r.mu.Lock()
	r.bindings[e.Path] = &bindingWithMeta{
		ID:      bindingID,
		Meta:    binding.Meta,
		Binding: binding,
	}
	r.mu.Unlock()

	r.logger.Debug("contract binding registered", zap.String("id", e.Path))
	r.sendAccept(e.Path)
}

func (r *ContractRegistry) updateBinding(e event.Event) {
	binding, ok := e.Data.(*contract.Binding)
	if !ok {
		r.sendReject(e.Path, "invalid binding payload")
		return
	}

	bindingID := registry.ParseID(e.Path)

	// Populate runtime fields
	binding.ID = bindingID
	if binding.Meta == nil {
		binding.Meta = make(registry.Metadata)
	}

	r.mu.Lock()
	r.bindings[e.Path] = &bindingWithMeta{
		ID:      bindingID,
		Meta:    binding.Meta,
		Binding: binding,
	}
	r.mu.Unlock()

	r.logger.Debug("contract binding updated", zap.String("id", e.Path))
	r.sendAccept(e.Path)
}

func (r *ContractRegistry) deleteBinding(e event.Event) {
	r.mu.Lock()
	delete(r.bindings, e.Path)
	r.mu.Unlock()

	r.logger.Debug("contract binding deleted", zap.String("id", e.Path))
	r.sendAccept(e.Path)
}

func (r *ContractRegistry) sendAccept(path event.Path) {
	r.bus.Send(r.ctx, event.Event{
		System: contract.System,
		Kind:   contract.Accept,
		Path:   path,
	})
}

func (r *ContractRegistry) sendReject(path event.Path, reason string) {
	r.bus.Send(r.ctx, event.Event{
		System: contract.System,
		Kind:   contract.Reject,
		Path:   path,
		Data:   reason,
	})
}

// GetContract implements contract.Registry interface
func (r *ContractRegistry) GetContract(ctx context.Context, id registry.ID) (contract.Contract, error) {
	r.mu.RLock()
	def, exists := r.definitions[id.String()]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("contract definition '%s' not found", id)
	}

	return &contractImpl{
		id:  id,
		def: def,
	}, nil
}

// GetBinding implements contract.Registry interface
func (r *ContractRegistry) GetBinding(ctx context.Context, id registry.ID) (*contract.Binding, error) {
	r.mu.RLock()
	bindingWithMeta, exists := r.bindings[id.String()]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("contract binding '%s' not found", id)
	}

	return bindingWithMeta.Binding, nil
}

// GetBindingsForContract implements contract.Registry interface
func (r *ContractRegistry) GetBindingsForContract(ctx context.Context, contractID registry.ID) ([]registry.ID, error) {
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

// contractImpl implements contract.Contract interface
type contractImpl struct {
	id  registry.ID
	def *contract.Definition
}

func (c *contractImpl) ID() registry.ID {
	return c.id
}

func (c *contractImpl) Meta() registry.Metadata {
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
	return nil, fmt.Errorf("method '%s' not found in contract '%s'", name, c.id)
}

// Ensure ContractRegistry implements contract.Registry interface
var _ contract.Registry = (*ContractRegistry)(nil)
