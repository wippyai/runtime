// Package di provides dependency injection service configuration.
package di

import (
	"encoding/json"

	"github.com/wippyai/runtime/api/contract"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

// Registry kind constants for contract entries
const (
	// Definition identifies a contract definition in the registry
	Definition registry.Kind = "contract.definition"

	// Binding identifies a contract binding in the registry
	Binding registry.Kind = "contract.binding"
)

// DefinitionConfig represents the configuration for a contract definition entry
// This is what gets unmarshaled from the YAML data field
type DefinitionConfig struct {
	ID      string         `json:"id,omitempty"` // ID for the definition, if specified within the data block
	Meta    attrs.Bag      `json:"meta,omitempty"`
	Methods []MethodConfig `json:"methods"`
}

// MethodConfig defines a single method in a contract
type MethodConfig struct {
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"` // Method-level description is kept
	InputSchemas  []SchemaConfig `json:"input_schemas,omitempty"`
	OutputSchemas []SchemaConfig `json:"output_schemas,omitempty"`
}

// SchemaConfig describes the format and schema of method arguments/returns
type SchemaConfig struct {
	Format     string          `json:"format"`     // MIME type: "application/schema+json", etc.
	Definition json.RawMessage `json:"definition"` // Actual schema (JSON Schema, etc.)
}

// BindingConfig represents the configuration for a contract binding entry
// This is what gets unmarshaled from the YAML data field
type BindingConfig struct {
	Meta      attrs.Bag             `json:"meta"`
	Contracts []BoundContractConfig `json:"contracts"`
}

// BoundContractConfig maps a contract to its implementation
type BoundContractConfig struct {
	Contract        string            `json:"contract"`          // Contract ID as string (will be parsed)
	Methods         map[string]string `json:"methods"`           // method_name -> function ID string
	ContextRequired []string          `json:"context_required"`  // Required scope keys
	Default         bool              `json:"default,omitempty"` // Whether this is the default binding for the contract
}

// Validate checks that DefinitionConfig is valid.
// Semantic validation (method names, schema formats) is done by the manager.
func (c *DefinitionConfig) Validate() error {
	return nil
}

// Validate checks that BindingConfig is valid.
// Semantic validation (contract exists, methods bound) is done by the manager.
func (c *BindingConfig) Validate() error {
	return nil
}

// ToDefinition converts a DefinitionConfig to a Definition
func (c *DefinitionConfig) ToDefinition() *contract.Definition {
	def := &contract.Definition{
		Methods: make([]contract.MethodDef, len(c.Methods)),
	}

	for i, methodCfg := range c.Methods {
		inputSchemas := make([]contract.SchemaDefinition, len(methodCfg.InputSchemas))
		for j, schemaCfg := range methodCfg.InputSchemas {
			inputSchemas[j] = contract.SchemaDefinition{
				Format:     schemaCfg.Format,
				Definition: schemaCfg.Definition,
			}
		}

		outputSchemas := make([]contract.SchemaDefinition, len(methodCfg.OutputSchemas))
		for j, schemaCfg := range methodCfg.OutputSchemas {
			outputSchemas[j] = contract.SchemaDefinition{
				Format:     schemaCfg.Format,
				Definition: schemaCfg.Definition,
			}
		}

		def.Methods[i] = contract.MethodDef{
			Name:          methodCfg.Name,
			Description:   methodCfg.Description, // Method-level description is still set
			InputSchemas:  inputSchemas,
			OutputSchemas: outputSchemas,
		}
	}

	return def
}

// ToBinding converts a BindingConfig to a Binding
func (c *BindingConfig) ToBinding() *contract.Binding {
	binding := &contract.Binding{
		Contracts: make([]contract.BoundContract, len(c.Contracts)),
	}

	for i, cfg := range c.Contracts {
		// Parse contract ID
		contractID := registry.ParseID(cfg.Contract)

		// Parse method IDs
		methods := make(map[string]registry.ID)
		for methodName, funcID := range cfg.Methods {
			methods[methodName] = registry.ParseID(funcID)
		}

		binding.Contracts[i] = contract.BoundContract{
			Contract:        contractID,
			Methods:         methods,
			ContextRequired: cfg.ContextRequired,
			Default:         cfg.Default,
		}
	}

	return binding
}
