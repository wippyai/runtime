// SPDX-License-Identifier: MPL-2.0

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
	Meta    attrs.Bag
	Binding *contract.Binding
	ID      registry.ID
}

// Registry implements contract.Registry interface for data management
type Registry struct {
	ctx             context.Context
	bus             event.Bus
	logger          *zap.Logger
	definitions     map[string]*contract.Definition
	bindings        map[string]*bindingWithMeta
	defaultBindings map[string]registry.ID
	subscriber      *eventbus.Subscriber
	mu              sync.RWMutex
}

const contractEventPattern = "contract.(definition|binding|changes).(register|update|delete|apply)"

// NewContractRegistry creates a new contract registry
func NewContractRegistry(bus event.Bus, logger *zap.Logger) *Registry {
	if logger == nil {
		logger = zap.NewNop()
	}
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
		contractEventPattern,
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

func (r *Registry) sendResponse(path event.Path, kind event.Kind, data any) {
	r.bus.Send(r.ctx, event.Event{
		System: contract.System,
		Kind:   kind,
		Path:   path,
		Data:   data,
	})
}

func (r *Registry) handleEvent(e event.Event) {
	switch e.Kind {
	case contract.ApplyChanges:
		changes, ok := e.Data.(contract.Changes)
		if !ok {
			r.logger.Error("invalid changes payload",
				zap.String("path", e.Path),
				zap.String("type", fmt.Sprintf("%T", e.Data)))
			r.sendReject(e.Path, "invalid changes payload")
			return
		}
		r.reply(e.Path, r.apply(changes))
	case contract.RegisterDefinition, contract.UpdateDefinition, contract.DeleteDefinition,
		contract.RegisterBinding, contract.UpdateBinding, contract.DeleteBinding:
		r.reply(e.Path, r.apply(contract.Changes{{Kind: e.Kind, Path: e.Path, Data: e.Data}}))
	default:
		r.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (r *Registry) reply(path event.Path, reason string) {
	if reason != "" {
		r.sendReject(path, reason)
		return
	}
	r.sendAccept(path)
}

// apply validates every change and then applies all of them under one lock,
// so a lookup observes either none or all of a batch. It returns the reason a
// batch is refused, or "" once it is applied.
func (r *Registry) apply(changes contract.Changes) string {
	for _, change := range changes {
		switch change.Kind {
		case contract.RegisterDefinition, contract.UpdateDefinition:
			if def, ok := change.Data.(*contract.Definition); !ok || def == nil {
				r.logger.Error("invalid definition payload",
					zap.String("path", change.Path),
					zap.String("type", fmt.Sprintf("%T", change.Data)))
				return "invalid definition payload"
			}
		case contract.RegisterBinding, contract.UpdateBinding:
			if binding, ok := change.Data.(*contract.Binding); !ok || binding == nil {
				r.logger.Error("invalid binding payload",
					zap.String("path", change.Path),
					zap.String("type", fmt.Sprintf("%T", change.Data)))
				return "invalid binding payload"
			}
		case contract.DeleteDefinition, contract.DeleteBinding:
		default:
			return "unknown change kind " + change.Kind
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	registered := make(map[string]struct{})
	for _, change := range changes {
		id := registry.ParseID(change.Path)
		switch change.Kind {
		case contract.RegisterDefinition, contract.UpdateDefinition:
			def := change.Data.(*contract.Definition)
			def.ID = id
			if def.Meta == nil {
				def.Meta = make(attrs.Bag)
			}
			r.definitions[change.Path] = def
			registered[change.Path] = struct{}{}
		case contract.DeleteDefinition:
			delete(r.definitions, change.Path)
			delete(r.defaultBindings, change.Path)
		case contract.RegisterBinding, contract.UpdateBinding:
			binding := change.Data.(*contract.Binding)
			binding.ID = id
			if binding.Meta == nil {
				binding.Meta = make(attrs.Bag)
			}
			r.removeDefaultBindings(id)
			r.bindings[change.Path] = &bindingWithMeta{ID: id, Meta: binding.Meta, Binding: binding}
			r.updateDefaultBindings(binding, id)
		case contract.DeleteBinding:
			r.removeDefaultBindings(id)
			delete(r.bindings, change.Path)
		}
		r.logger.Debug("contract change applied", zap.String("kind", change.Kind), zap.String("id", change.Path))
	}
	r.restoreDefaultBindings(registered)
	return ""
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

// restoreDefaultBindings gives each definition registered in a batch the
// default binding a retained binding declares for it, which a delete of the
// same definition earlier in the batch cleared. Assumes r.mu is already locked.
func (r *Registry) restoreDefaultBindings(registered map[string]struct{}) {
	if len(registered) == 0 {
		return
	}
	for _, binding := range r.bindings {
		for _, bc := range binding.Binding.Contracts {
			contractID := bc.Contract.String()
			if _, ok := registered[contractID]; ok && bc.Default {
				r.defaultBindings[contractID] = binding.ID
			}
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
	r.sendResponse(path, contract.ContractAccept, nil)
}

func (r *Registry) sendReject(path event.Path, reason string) {
	r.sendResponse(path, contract.ContractReject, reason)
}

// GetContract implements contract.Registry interface
func (r *Registry) GetContract(_ context.Context, id registry.ID) (contract.Contract, error) {
	r.mu.RLock()
	def, exists := r.definitions[id.String()]
	r.mu.RUnlock()

	if !exists {
		return nil, NewContractNotFoundError(id)
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
		return nil, NewBindingNotFoundError(id)
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
		return registry.NewID("", ""), NewNoDefaultBindingError(contractID)
	}

	return bindingID, nil
}

// contractImpl implements contract.Contract interface
type contractImpl struct {
	def *contract.Definition
	id  registry.ID
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
	return nil, NewMethodNotFoundError(name, c.id)
}

// Ensure Registry implements contract.Registry interface
var _ contract.Registry = (*Registry)(nil)
