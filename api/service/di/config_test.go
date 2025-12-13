// Package di provides dependency injection service configuration.
package di

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

func TestKindConstants(t *testing.T) {
	tests := []struct {
		name     string
		kind     registry.Kind
		expected string
	}{
		{"definition", KindDefinition, "contract.definition"},
		{"binding", KindBinding, "contract.binding"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.kind)
		})
	}
}

func TestDefinitionConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  DefinitionConfig
		wantErr bool
	}{
		{
			name: "complete definition config",
			config: DefinitionConfig{
				ID:   "payment-contract",
				Meta: attrs.Bag{"version": "1.0"},
				Methods: []MethodConfig{
					{
						Name:        "process",
						Description: "Process payment",
						InputSchemas: []SchemaConfig{
							{Format: "application/json", Definition: json.RawMessage(`{"type":"object"}`)},
						},
						OutputSchemas: []SchemaConfig{
							{Format: "application/json", Definition: json.RawMessage(`{"type":"object"}`)},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "minimal definition config",
			config: DefinitionConfig{
				Methods: []MethodConfig{
					{Name: "test"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded DefinitionConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config.ID, decoded.ID)
			assert.Equal(t, len(tt.config.Methods), len(decoded.Methods))
		})
	}
}

func TestMethodConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  MethodConfig
		wantErr bool
	}{
		{
			name: "complete method config",
			config: MethodConfig{
				Name:        "getData",
				Description: "Get data",
				InputSchemas: []SchemaConfig{
					{Format: "application/json", Definition: json.RawMessage(`{"type":"string"}`)},
				},
				OutputSchemas: []SchemaConfig{
					{Format: "application/json", Definition: json.RawMessage(`{"type":"array"}`)},
				},
			},
			wantErr: false,
		},
		{
			name: "minimal method config",
			config: MethodConfig{
				Name: "ping",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded MethodConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config.Name, decoded.Name)
		})
	}
}

func TestSchemaConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  SchemaConfig
		wantErr bool
	}{
		{
			name: "json schema",
			config: SchemaConfig{
				Format:     "application/schema+json",
				Definition: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}}}`),
			},
			wantErr: false,
		},
		{
			name: "simple schema",
			config: SchemaConfig{
				Format:     "text/plain",
				Definition: json.RawMessage(`"simple"`),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded SchemaConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config.Format, decoded.Format)
		})
	}
}

func TestBindingConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  BindingConfig
		wantErr bool
	}{
		{
			name: "complete binding config",
			config: BindingConfig{
				Meta: attrs.Bag{"env": "production"},
				Contracts: []BoundContractConfig{
					{
						Contract: "contracts:payment",
						Methods: map[string]string{
							"process": "functions:process_payment",
						},
						ContextRequired: []string{"user", "session"},
						Default:         true,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "minimal binding config",
			config: BindingConfig{
				Contracts: []BoundContractConfig{
					{
						Contract: "test",
						Methods:  map[string]string{},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded BindingConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, len(tt.config.Contracts), len(decoded.Contracts))
		})
	}
}

func TestBoundContractConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  BoundContractConfig
		wantErr bool
	}{
		{
			name: "complete bound contract config",
			config: BoundContractConfig{
				Contract: "contracts:api",
				Methods: map[string]string{
					"get":  "funcs:get_handler",
					"post": "funcs:post_handler",
				},
				ContextRequired: []string{"auth", "scope"},
				Default:         true,
			},
			wantErr: false,
		},
		{
			name: "minimal bound contract config",
			config: BoundContractConfig{
				Contract: "test",
				Methods:  map[string]string{"test": "func"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded BoundContractConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config.Contract, decoded.Contract)
		})
	}
}

func TestDefinitionConfig_ToDefinition(t *testing.T) {
	config := DefinitionConfig{
		Methods: []MethodConfig{
			{
				Name:        "test",
				Description: "Test method",
				InputSchemas: []SchemaConfig{
					{Format: "application/json", Definition: json.RawMessage(`{}`)},
				},
			},
		},
	}

	def := config.ToDefinition()
	assert.NotNil(t, def)
	assert.Len(t, def.Methods, 1)
	assert.Equal(t, "test", def.Methods[0].Name)
	assert.Equal(t, "Test method", def.Methods[0].Description)
}

func TestBindingConfig_ToBinding(t *testing.T) {
	config := BindingConfig{
		Contracts: []BoundContractConfig{
			{
				Contract: "contracts:test",
				Methods: map[string]string{
					"method": "funcs:handler",
				},
				Default: true,
			},
		},
	}

	binding := config.ToBinding()
	assert.NotNil(t, binding)
	assert.Len(t, binding.Contracts, 1)
	assert.True(t, binding.Contracts[0].Default)
}

func TestDefinitionConfig_Validate(t *testing.T) {
	// Validate() is a no-op for DefinitionConfig.
	// Semantic validation is done by the DI manager.
	config := DefinitionConfig{}
	assert.NoError(t, config.Validate())
}

func TestBindingConfig_Validate(t *testing.T) {
	// Validate() is a no-op for BindingConfig.
	// Semantic validation is done by the DI manager.
	config := BindingConfig{}
	assert.NoError(t, config.Validate())
}
