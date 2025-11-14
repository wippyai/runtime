// Package contract provides contract and service definitions.
package contract

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
)

func TestEventConstants(t *testing.T) {
	tests := []struct {
		name     string
		system   event.System
		kind     event.Kind
		expected string
	}{
		{"system", System, "", "contract"},
		{"register definition", "", RegisterDefinition, "contract.definition.register"},
		{"update definition", "", UpdateDefinition, "contract.definition.update"},
		{"delete definition", "", DeleteDefinition, "contract.definition.delete"},
		{"register binding", "", RegisterBinding, "contract.binding.register"},
		{"update binding", "", UpdateBinding, "contract.binding.update"},
		{"delete binding", "", DeleteBinding, "contract.binding.delete"},
		{"accept", "", Accept, "contract.accept"},
		{"reject", "", Reject, "contract.reject"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.system != "" {
				assert.Equal(t, tt.expected, string(tt.system))
			}
			if tt.kind != "" {
				assert.Equal(t, tt.expected, string(tt.kind))
			}
		})
	}
}

func TestDefinition_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		def     Definition
		wantErr bool
	}{
		{
			name: "complete definition",
			def: Definition{
				ID:   registry.ID{NS: "contracts", Name: "payment"},
				Meta: registry.Metadata{"version": "1.0"},
				Methods: []MethodDef{
					{
						Name:        "process",
						Description: "Process payment",
						InputSchemas: []SchemaDefinition{
							{Format: "application/schema+json", Definition: map[string]any{"type": "object"}},
						},
						OutputSchemas: []SchemaDefinition{
							{Format: "application/schema+json", Definition: map[string]any{"type": "object"}},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "minimal definition",
			def: Definition{
				Methods: []MethodDef{
					{Name: "execute"},
				},
			},
			wantErr: false,
		},
		{
			name: "empty methods",
			def: Definition{
				ID:      registry.ID{NS: "test", Name: "contract"},
				Methods: []MethodDef{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.def)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded Definition
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.def, decoded)
		})
	}
}

func TestMethodDef_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		method  MethodDef
		wantErr bool
	}{
		{
			name: "complete method",
			method: MethodDef{
				Name:        "getData",
				Description: "Retrieve data",
				InputSchemas: []SchemaDefinition{
					{Format: "application/json", Definition: map[string]any{"type": "string"}},
				},
				OutputSchemas: []SchemaDefinition{
					{Format: "application/json", Definition: map[string]any{"type": "array"}},
				},
			},
			wantErr: false,
		},
		{
			name: "minimal method",
			method: MethodDef{
				Name: "ping",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.method)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded MethodDef
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.method, decoded)
		})
	}
}

func TestSchemaDefinition_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		schema  SchemaDefinition
		wantErr bool
	}{
		{
			name: "json schema",
			schema: SchemaDefinition{
				Format:     "application/schema+json",
				Definition: map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}},
			},
			wantErr: false,
		},
		{
			name: "string definition",
			schema: SchemaDefinition{
				Format:     "text/plain",
				Definition: "simple string schema",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.schema)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded SchemaDefinition
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.schema, decoded)
		})
	}
}

func TestBinding_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		binding Binding
		wantErr bool
	}{
		{
			name: "complete binding",
			binding: Binding{
				ID:   registry.ID{NS: "bindings", Name: "impl1"},
				Meta: registry.Metadata{"env": "production"},
				Contracts: []BoundContract{
					{
						Contract: registry.ID{NS: "contracts", Name: "payment"},
						Methods: map[string]registry.ID{
							"process": {NS: "functions", Name: "process_payment"},
						},
						ContextRequired: []string{"user", "session"},
						Default:         true,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "minimal binding",
			binding: Binding{
				Contracts: []BoundContract{
					{
						Contract: registry.ID{NS: "c", Name: "test"},
						Methods:  map[string]registry.ID{},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.binding)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded Binding
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.binding, decoded)
		})
	}
}

func TestBoundContract_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		contract BoundContract
		wantErr  bool
	}{
		{
			name: "complete bound contract",
			contract: BoundContract{
				Contract: registry.ID{NS: "contracts", Name: "api"},
				Methods: map[string]registry.ID{
					"get":    {NS: "funcs", Name: "get_handler"},
					"post":   {NS: "funcs", Name: "post_handler"},
					"delete": {NS: "funcs", Name: "delete_handler"},
				},
				ContextRequired: []string{"auth", "scope"},
				Default:         true,
			},
			wantErr: false,
		},
		{
			name: "non-default binding",
			contract: BoundContract{
				Contract:        registry.ID{NS: "c", Name: "test"},
				Methods:         map[string]registry.ID{"test": {NS: "f", Name: "t"}},
				ContextRequired: nil,
				Default:         false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.contract)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded BoundContract
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.contract, decoded)
		})
	}
}

func TestContext_Registry(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		reg := GetRegistry(ctx)
		assert.Nil(t, reg)

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		type mockInstantiator struct{ Instantiator }
		mockInst := &mockInstantiator{}

		ctx = WithContracts(ctx, mockReg, mockInst)

		retrieved := GetRegistry(ctx)
		assert.Equal(t, mockReg, retrieved)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		reg := GetRegistry(ctx)
		assert.Nil(t, reg)

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		type mockInstantiator struct{ Instantiator }
		mockInst := &mockInstantiator{}

		ctx = WithContracts(ctx, mockReg, mockInst)

		reg = GetRegistry(ctx)
		assert.Equal(t, mockReg, reg)
	})
}

func TestContext_Instantiator(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		inst := GetInstantiator(ctx)
		assert.Nil(t, inst)

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		type mockInstantiator struct{ Instantiator }
		mockInst := &mockInstantiator{}

		ctx = WithContracts(ctx, mockReg, mockInst)

		retrieved := GetInstantiator(ctx)
		assert.Equal(t, mockInst, retrieved)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		inst := GetInstantiator(ctx)
		assert.Nil(t, inst)

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		type mockInstantiator struct{ Instantiator }
		mockInst := &mockInstantiator{}

		ctx = WithContracts(ctx, mockReg, mockInst)

		inst = GetInstantiator(ctx)
		assert.Equal(t, mockInst, inst)
	})
}
