package di

import (
	"encoding/json"
	"github.com/ponyruntime/pony/api/contract"

	"github.com/ponyruntime/pony/api/registry"
)

// Registry kind constants for contract entries
const (
	// KindDefinition identifies a contract definition in the registry
	KindDefinition registry.Kind = "contract.definition"

	// KindBinding identifies a contract binding in the registry
	KindBinding registry.Kind = "contract.binding"
)

// DefinitionConfig represents the configuration for a contract definition entry
// This is what gets unmarshaled from the YAML data field
type DefinitionConfig struct {
	Meta        registry.Metadata `json:"meta"`
	Description string            `json:"description,omitempty"`
	Methods     []MethodConfig    `json:"methods"`
}

// MethodConfig defines a single method in a contract
type MethodConfig struct {
	Name         string       `json:"name"`
	Description  string       `json:"description,omitempty"`
	InputSchema  SchemaConfig `json:"input_schema"`
	OutputSchema SchemaConfig `json:"output_schema,omitempty"`
}

// SchemaConfig describes the format and schema of method arguments/returns
type SchemaConfig struct {
	Format     string          `json:"format"`     // MIME type: "application/schema+json", etc.
	Definition json.RawMessage `json:"definition"` // Actual schema (JSON Schema, etc.)
}

// BindingConfig represents the configuration for a contract binding entry
// This is what gets unmarshaled from the YAML data field
type BindingConfig struct {
	Meta      registry.Metadata     `json:"meta"`
	Contracts []BoundContractConfig `json:"contracts"`
}

// BoundContractConfig maps a contract to its implementation
type BoundContractConfig struct {
	Contract      string            `json:"contract"`       // Contract ID as string (will be parsed)
	Methods       map[string]string `json:"methods"`        // method_name -> function ID string
	ScopeRequired []string          `json:"scope_required"` // Required scope keys
}

// ToDefinition converts a DefinitionConfig to a Definition
func (c *DefinitionConfig) ToDefinition() *contract.Definition {
	def := &contract.Definition{
		Description: c.Description,
		Methods:     make([]contract.MethodDef, len(c.Methods)),
	}

	for i, method := range c.Methods {
		def.Methods[i] = contract.MethodDef{
			Name:        method.Name,
			Description: method.Description,
			InputSchema: contract.SchemaDefinition{
				Format:     method.InputSchema.Format,
				Definition: method.InputSchema.Definition,
			},
			OutputSchema: contract.SchemaDefinition{
				Format:     method.OutputSchema.Format,
				Definition: method.OutputSchema.Definition,
			},
		}
	}

	return def
}

// ToBinding converts a BindingConfig to a Binding
func (c *BindingConfig) ToBinding() *contract.Binding {
	binding := &contract.Binding{
		Contracts: make([]contract.BoundContract, len(c.Contracts)),
	}

	for i, c := range c.Contracts {
		// Parse contract ID
		contractID := registry.ParseID(c.Contract)

		// Parse method IDs
		methods := make(map[string]registry.ID)
		for methodName, funcID := range c.Methods {
			methods[methodName] = registry.ParseID(funcID)
		}

		binding.Contracts[i] = contract.BoundContract{
			Contract:      contractID,
			Methods:       methods,
			ScopeRequired: c.ScopeRequired,
		}
	}

	return binding
}
