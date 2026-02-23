// SPDX-License-Identifier: MPL-2.0

// Package contract provides contract and service definitions.
package contract

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
)

// System identifies the contract system in the event bus.
const System event.System = "contract"

// Event kinds for contract operations.
const (
	RegisterDefinition event.Kind = "contract.definition.register"
	UpdateDefinition   event.Kind = "contract.definition.update"
	DeleteDefinition   event.Kind = "contract.definition.delete"
	RegisterBinding    event.Kind = "contract.binding.register"
	UpdateBinding      event.Kind = "contract.binding.update"
	DeleteBinding      event.Kind = "contract.binding.delete"
	ContractAccept     event.Kind = "contract.accept"
	ContractReject     event.Kind = "contract.reject"
)

type (
	// Definition represents a contract stored in the registry.
	Definition struct {
		ID      registry.ID `json:"id,omitempty"`
		Meta    attrs.Bag   `json:"meta,omitempty"`
		Methods []MethodDef `json:"methods"`
	}

	// MethodDef defines a single method in a contract.
	MethodDef struct {
		Name          string             `json:"name"`
		Description   string             `json:"description,omitempty"`
		InputSchemas  []SchemaDefinition `json:"input_schemas,omitempty"`
		OutputSchemas []SchemaDefinition `json:"output_schemas,omitempty"`
	}

	// SchemaDefinition describes the format and schema of method arguments/returns.
	SchemaDefinition struct {
		Definition any    `json:"definition"`
		Format     string `json:"format"`
	}

	// Binding represents an implementation binding stored in the registry.
	Binding struct {
		ID        registry.ID     `json:"id,omitempty"`
		Meta      attrs.Bag       `json:"meta,omitempty"`
		Contracts []BoundContract `json:"contracts"`
	}

	// BoundContract maps a contract to its implementation.
	BoundContract struct {
		Contract        registry.ID            `json:"contract"`
		Methods         map[string]registry.ID `json:"methods"`
		ContextRequired []string               `json:"context_required"`
		Default         bool                   `json:"default,omitempty"`
	}

	// Registry defines the interface for managing contracts and their bindings.
	Registry interface {
		// GetContract loads a contract definition by ID.
		GetContract(ctx context.Context, id registry.ID) (Contract, error)

		// GetBinding loads a contract binding by ID.
		GetBinding(ctx context.Context, id registry.ID) (*Binding, error)

		// GetBindingsForContract returns all binding IDs that implement the specified contract.
		GetBindingsForContract(ctx context.Context, contractID registry.ID) ([]registry.ID, error)

		// GetDefaultBinding returns the default binding ID for the specified contract.
		GetDefaultBinding(ctx context.Context, contractID registry.ID) (registry.ID, error)
	}

	// Instantiator handles creating contract instances from bindings.
	Instantiator interface {
		// Instantiate creates a new contract instance with the given binding ID and scope.
		Instantiate(ctx context.Context, bindingID registry.ID, bindContext attrs.Bag) (Instance, error)
	}

	// Contract represents a loaded contract definition at runtime.
	Contract interface {
		// ID returns the contract's registry ID.
		ID() registry.ID

		// Meta returns the contract's metadata.
		Meta() attrs.Bag

		// Methods returns all method definitions.
		Methods() []MethodDef

		// Method returns a specific method definition.
		Method(name string) (*MethodDef, error)
	}

	// Instance represents an opened contract bound to an implementation.
	Instance interface {
		// ID returns the binding ID used to create this instance.
		ID() registry.ID

		// Implements returns the list of contracts this instance implements.
		Implements() []Contract

		// Call invokes a method on this instance synchronously.
		Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error)
	}
)
