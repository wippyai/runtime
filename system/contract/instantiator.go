package contract

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
)

// Instantiator implements contract.Instantiator interface for runtime execution
type Instantiator struct {
	registry contract.Registry
	funcReg  function.Registry
}

// NewContractInstantiator creates a new contract instantiator
func NewContractInstantiator(registry contract.Registry, funcReg function.Registry) *Instantiator {
	return &Instantiator{
		registry: registry,
		funcReg:  funcReg,
	}
}

// Instantiate implements contract.Instantiator interface
func (i *Instantiator) Instantiate(ctx context.Context, bindingID registry.ID, scope registry.Metadata) (contract.Instance, error) {
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

// instanceImpl implements contract.Instance interface
type instanceImpl struct {
	id        registry.ID
	binding   *contract.Binding
	contracts []contract.Contract
	context   registry.Metadata
	funcReg   function.Registry
}

func (i *instanceImpl) Implements() []contract.Contract {
	return i.contracts
}

func (i *instanceImpl) ID() registry.ID {
	return i.id
}

func (i *instanceImpl) Call(ctx context.Context, method string, args payload.Payloads) (*runtime.Result, error) {
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

	// Validate required context keys - now checks BOTH scope and Go context
	if err := i.validateContext(ctx, boundContract.ContextRequired); err != nil {
		return nil, err
	}

	// Create task with payloads
	task := runtime.Task{
		ID:       funcID,
		Payloads: args,
	}

	// Merge context values and pass via task.Context
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

		// Pass merged values via task.Context so they propagate through AcquireFrameContext
		task.Context = []ctxapi.Pair{ctxapi.ValuesPair(values)}
	}

	// Call the function with context
	return i.funcReg.Call(ctx, task)
}

// validateContext checks that all required context keys are present in EITHER scope OR Go context
// This fixes the bug where validation only checked scope but execution had access to both
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
