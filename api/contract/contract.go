// Package contract provides contract and service definitions.
package contract

import (
	"context"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
)

const (
	// System identifies the contract system in the event bus
	System event.System = "contract"

	// Event kinds for contract definitions
	RegisterDefinition event.Kind = "contract.definition.register"
	UpdateDefinition   event.Kind = "contract.definition.update"
	DeleteDefinition   event.Kind = "contract.definition.delete"
	RegisterBinding    event.Kind = "contract.binding.register"
	UpdateBinding      event.Kind = "contract.binding.update"
	DeleteBinding      event.Kind = "contract.binding.delete"
	Accept             event.Kind = "contract.accept"
	Reject             event.Kind = "contract.reject"
)

// Contract definition structures (stored in registry)

type (
	// Definition represents a contract stored in the registry
	// This is the actual data payload for kind: contract.definition
	Definition struct {
		ID      registry.ID       `json:"id,omitempty"`   // Contract definition registry ID (populated at runtime)
		Meta    registry.Metadata `json:"meta,omitempty"` // Contract definition metadata (populated at runtime)
		Methods []MethodDef       `json:"methods"`
	}

	// MethodDef defines a single method in a contract
	MethodDef struct {
		Name          string             `json:"name"`
		Description   string             `json:"description,omitempty"` // Method-level description is kept
		InputSchemas  []SchemaDefinition `json:"input_schemas,omitempty"`
		OutputSchemas []SchemaDefinition `json:"output_schemas,omitempty"`
	}

	// SchemaDefinition describes the format and schema of method arguments/returns
	SchemaDefinition struct {
		Format     string `json:"format"`     // MIME type: "application/schema+json", etc.
		Definition any    `json:"definition"` // Actual schema (JSON Schema, etc.)
	}
)

// Contract binding structures (stored in registry)

type (
	// Binding represents an implementation binding stored in the registry
	// This is the actual data payload for kind: contract.binding
	Binding struct {
		ID        registry.ID       `json:"id,omitempty"`   // Binding registry ID (populated at runtime)
		Meta      registry.Metadata `json:"meta,omitempty"` // Binding metadata (populated at runtime)
		Contracts []BoundContract   `json:"contracts"`
	}

	// BoundContract maps a contract to its implementation
	BoundContract struct {
		Contract        registry.ID            `json:"contract"`          // Reference to contract definition
		Methods         map[string]registry.ID `json:"methods"`           // method_name -> function ID
		ContextRequired []string               `json:"context_required"`  // Required scope keys
		Default         bool                   `json:"default,omitempty"` // Whether this is the default binding for the contract
	}
)

// Registry and runtime interfaces

type (
	// Registry defines the interface for managing contracts and their bindings.
	// It provides methods for loading contracts and bindings.
	Registry interface {
		// GetContract loads a contract definition by ID
		GetContract(ctx context.Context, id registry.ID) (Contract, error)

		// GetBinding loads a contract binding by ID
		GetBinding(ctx context.Context, id registry.ID) (*Binding, error)

		// GetBindingsForContract returns all binding IDs that implement the specified contract
		GetBindingsForContract(ctx context.Context, contractID registry.ID) ([]registry.ID, error)

		// GetDefaultBinding returns the default binding ID for the specified contract
		GetDefaultBinding(ctx context.Context, contractID registry.ID) (registry.ID, error)
	}

	// Instantiator handles creating contract instances from bindings
	Instantiator interface {
		// Instantiate creates a new contract instance with the given binding ID and scope
		Instantiate(
			ctx context.Context,
			bindingID registry.ID,
			bindContext registry.Metadata,
		) (Instance, error)
	}

	// Contract represents a loaded contract definition at runtime
	Contract interface {
		// ID returns the contract's registry ID
		ID() registry.ID

		// Meta returns the contract's metadata
		Meta() registry.Metadata

		// Methods returns all method definitions
		Methods() []MethodDef

		// Method returns a specific method definition
		Method(name string) (*MethodDef, error)
	}

	// Instance represents an opened contract bound to an implementation
	Instance interface {
		// ID returns the binding ID used to create this instance
		ID() registry.ID

		// Implements returns the list of contracts this instance implements
		Implements() []Contract

		// Call invokes a method on this instance
		Call(ctx context.Context, method string, input payload.Payloads) (chan *runtime.Result, error)
	}
)
