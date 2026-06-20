// SPDX-License-Identifier: MPL-2.0

package contract

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	secapi "github.com/wippyai/runtime/api/security"
)

// Instantiator implements contract.Instantiator interface for runtime execution.
type Instantiator struct {
	registry contract.Registry
	funcReg  function.Registry
}

// NewContractInstantiator creates a new contract instantiator.
func NewContractInstantiator(registry contract.Registry, funcReg function.Registry) *Instantiator {
	return &Instantiator{
		registry: registry,
		funcReg:  funcReg,
	}
}

// Instantiate implements contract.Instantiator interface.
func (i *Instantiator) Instantiate(ctx context.Context, bindingID registry.ID, scope attrs.Bag) (contract.Instance, error) {
	binding, err := i.registry.GetBinding(ctx, bindingID)
	if err != nil {
		return nil, err
	}

	contracts := make([]contract.Contract, 0, len(binding.Contracts))
	for _, bc := range binding.Contracts {
		contractObj, err := i.registry.GetContract(ctx, bc.Contract)
		if err != nil {
			return nil, NewContractLoadError(bc.Contract, err)
		}
		contracts = append(contracts, contractObj)
	}

	return &instanceImpl{
		id:        bindingID,
		binding:   binding,
		contracts: contracts,
		context:   scope,
		funcReg:   i.funcReg,
	}, nil
}

// instanceImpl implements contract.Instance interface.
type instanceImpl struct {
	funcReg   function.Registry
	secScope  secapi.Scope
	binding   *contract.Binding
	context   attrs.Bag
	id        registry.ID
	actor     secapi.Actor
	contracts []contract.Contract
	hasActor  bool
	hasScope  bool
}

// securityFramer is implemented by contract instances that can run their bound
// functions under an explicit open-time security context (the actor/scope set on
// the contract wrapper via with_actor/with_scope). handleOpen frames the instance
// through this capability rather than coupling to a concrete type.
type securityFramer interface {
	frameSecurity(actor secapi.Actor, hasActor bool, scope secapi.Scope, hasScope bool)
}

func (i *instanceImpl) frameSecurity(actor secapi.Actor, hasActor bool, scope secapi.Scope, hasScope bool) {
	i.actor, i.hasActor = actor, hasActor
	i.secScope, i.hasScope = scope, hasScope
}

func (i *instanceImpl) Implements() []contract.Contract {
	return i.contracts
}

func (i *instanceImpl) ID() registry.ID {
	return i.id
}

func (i *instanceImpl) Call(ctx context.Context, method string, args payload.Payloads, options runtime.Options) (*runtime.Result, error) {
	// Find the bound contract and method
	var funcID registry.ID
	var boundContract contract.BoundContract
	var found bool

	for _, bc := range i.binding.Contracts {
		if methodFunc, exists := bc.Methods[method]; exists {
			funcID = methodFunc
			boundContract = bc
			found = true
			break
		}
	}

	if !found {
		return nil, NewMethodNotBoundError(method)
	}

	// Validate required context keys in scope or Go context.
	if err := i.validateContext(ctx, boundContract.ContextRequired); err != nil {
		return nil, err
	}

	// Create task with payloads and options.
	task := runtime.Task{
		ID:       funcID,
		Payloads: args,
		Options:  options,
	}

	// Build the context overrides applied to the bound function's FrameContext:
	// scope values, plus the actor/scope the instance was opened with so the
	// function runs under the framed security context (the same Task.Context pair
	// mechanism funcs uses for its with_actor/with_scope).
	var pairs []ctxapi.Pair

	if len(i.context) > 0 {
		// Get existing values from FrameContext or create new
		values := ctxapi.GetValues(ctx)
		if values != nil {
			// Clone existing values to avoid mutation
			values = values.Clone().(ctxapi.Values)
		} else {
			values = ctxapi.NewValues()
		}

		// Merge scope context values (scope wins over existing)
		for k, v := range i.context {
			values.Set(k, v)
		}

		pairs = append(pairs, ctxapi.ValuesPair(values))
	}

	if i.hasActor {
		pairs = append(pairs, secapi.ActorPair(i.actor))
	}
	if i.hasScope {
		pairs = append(pairs, secapi.ScopePair(i.secScope))
	}

	if len(pairs) > 0 {
		task.Context = pairs
	}

	// Call the function with context
	return i.funcReg.Call(ctx, task)
}

// validateContext checks that all required context keys are present in scope or Go context.
func (i *instanceImpl) validateContext(ctx context.Context, requiredKeys []string) error {
	if len(requiredKeys) == 0 {
		return nil
	}

	var missing []string
	for _, key := range requiredKeys {
		found := false

		// First check scope (i.context)
		if i.context != nil {
			if _, exists := i.context[key]; exists {
				found = true
			}
		}

		// If not found in scope, check Go context values
		if !found {
			if values := ctxapi.GetValues(ctx); values != nil {
				if _, exists := values.Get(key); exists {
					found = true
				}
			}
		}

		if !found {
			missing = append(missing, key)
		}
	}

	if len(missing) > 0 {
		return NewMissingContextKeysError(missing)
	}

	return nil
}

// Ensure Instantiator implements contract.Instantiator interface
var _ contract.Instantiator = (*Instantiator)(nil)

// Ensure instanceImpl implements contract.Instance interface
var _ contract.Instance = (*instanceImpl)(nil)
