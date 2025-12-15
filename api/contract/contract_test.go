// Package contract provides contract and service definitions.
package contract

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
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
		{"register definition", "", KindRegisterDefinition, "contract.definition.register"},
		{"update definition", "", KindUpdateDefinition, "contract.definition.update"},
		{"delete definition", "", KindDeleteDefinition, "contract.definition.delete"},
		{"register binding", "", KindRegisterBinding, "contract.binding.register"},
		{"update binding", "", KindUpdateBinding, "contract.binding.update"},
		{"delete binding", "", KindDeleteBinding, "contract.binding.delete"},
		{"accept", "", KindAccept, "contract.accept"},
		{"reject", "", KindReject, "contract.reject"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.system != "" {
				assert.Equal(t, tt.expected, tt.system)
			}
			if tt.kind != "" {
				assert.Equal(t, tt.expected, tt.kind)
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
				ID:   registry.NewID("contracts", "payment"),
				Meta: attrs.Bag{"version": "1.0"},
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
				ID: registry.NewID("", ""),
				Methods: []MethodDef{
					{Name: "execute"},
				},
			},
			wantErr: false,
		},
		{
			name: "empty methods",
			def: Definition{
				ID:      registry.NewID("test", "contract"),
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
				ID:   registry.NewID("bindings", "impl1"),
				Meta: attrs.Bag{"env": "production"},
				Contracts: []BoundContract{
					{
						Contract: registry.NewID("contracts", "payment"),
						Methods: map[string]registry.ID{
							"process": registry.NewID("functions", "process_payment"),
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
				ID: registry.NewID("", ""),
				Contracts: []BoundContract{
					{
						Contract: registry.NewID("c", "test"),
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
				Contract: registry.NewID("contracts", "api"),
				Methods: map[string]registry.ID{
					"get":    registry.NewID("funcs", "get_handler"),
					"post":   registry.NewID("funcs", "post_handler"),
					"delete": registry.NewID("funcs", "delete_handler"),
				},
				ContextRequired: []string{"auth", "scope"},
				Default:         true,
			},
			wantErr: false,
		},
		{
			name: "non-default binding",
			contract: BoundContract{
				Contract:        registry.NewID("c", "test"),
				Methods:         map[string]registry.ID{"test": registry.NewID("f", "t")},
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

func TestCommandPools(t *testing.T) {
	t.Run("OpenCmd", func(t *testing.T) {
		cmd := AcquireOpenCmd()
		assert.NotNil(t, cmd)
		assert.Equal(t, CmdOpen, cmd.CmdID())

		cmd.BindingID = registry.NewID("test", "binding")
		cmd.Scope = attrs.NewBag()
		cmd.Scope.Set("key", "value")
		cmd.HasActor = true
		cmd.HasScope = true

		cmd.Release()

		cmd2 := AcquireOpenCmd()
		assert.NotNil(t, cmd2)
		assert.Equal(t, registry.ID{}, cmd2.BindingID)
		assert.Nil(t, cmd2.Scope)
		assert.False(t, cmd2.HasActor)
		assert.False(t, cmd2.HasScope)
	})

	t.Run("CallCmd", func(t *testing.T) {
		cmd := AcquireCallCmd()
		assert.NotNil(t, cmd)
		assert.Equal(t, CmdCall, cmd.CmdID())

		cmd.Method = "testMethod"

		cmd.Release()

		cmd2 := AcquireCallCmd()
		assert.NotNil(t, cmd2)
		assert.Equal(t, "", cmd2.Method)
		assert.Nil(t, cmd2.Instance)
		assert.Nil(t, cmd2.Args)
	})

	t.Run("AsyncCallCmd", func(t *testing.T) {
		cmd := AcquireAsyncCallCmd()
		assert.NotNil(t, cmd)
		assert.Equal(t, CmdAsyncCall, cmd.CmdID())

		cmd.Method = "asyncMethod"
		cmd.Topic = "result-topic"

		cmd.Release()

		cmd2 := AcquireAsyncCallCmd()
		assert.NotNil(t, cmd2)
		assert.Equal(t, "", cmd2.Method)
		assert.Equal(t, "", cmd2.Topic)
		assert.Nil(t, cmd2.Instance)
		assert.Nil(t, cmd2.Args)
	})

	t.Run("AsyncCancelCmd", func(t *testing.T) {
		cmd := AcquireAsyncCancelCmd()
		assert.NotNil(t, cmd)
		assert.Equal(t, CmdAsyncCancel, cmd.CmdID())

		cmd.Topic = "cancel-topic"

		cmd.Release()

		cmd2 := AcquireAsyncCancelCmd()
		assert.NotNil(t, cmd2)
		assert.Equal(t, "", cmd2.Topic)
	})
}

func TestCommandIDs(t *testing.T) {
	assert.Equal(t, CmdOpen, dispatcher.CommandID(300))
	assert.Equal(t, CmdCall, dispatcher.CommandID(301))
	assert.Equal(t, CmdAsyncCall, dispatcher.CommandID(302))
	assert.Equal(t, CmdAsyncCancel, dispatcher.CommandID(303))
}

func TestResultTypes(t *testing.T) {
	t.Run("OpenResult", func(t *testing.T) {
		result := OpenResult{Instance: nil, Error: nil}
		assert.Nil(t, result.Instance)
		assert.NoError(t, result.Error)
	})

	t.Run("CallResult", func(t *testing.T) {
		result := CallResult{Value: "test", Error: nil}
		assert.Equal(t, "test", result.Value)
		assert.NoError(t, result.Error)
	})

	t.Run("AsyncCallResult", func(t *testing.T) {
		result := AsyncCallResult{Error: nil}
		assert.NoError(t, result.Error)
	})
}

func TestContext_NoAppContext(t *testing.T) {
	t.Run("GetRegistry_NoAppContext", func(t *testing.T) {
		ctx := context.Background()
		reg := GetRegistry(ctx)
		assert.Nil(t, reg)
	})

	t.Run("GetInstantiator_NoAppContext", func(t *testing.T) {
		ctx := context.Background()
		inst := GetInstantiator(ctx)
		assert.Nil(t, inst)
	})

	t.Run("WithContracts_NoAppContext", func(t *testing.T) {
		ctx := context.Background()

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		type mockInstantiator struct{ Instantiator }
		mockInst := &mockInstantiator{}

		result := WithContracts(ctx, mockReg, mockInst)
		assert.Equal(t, ctx, result)

		reg := GetRegistry(result)
		assert.Nil(t, reg)
	})
}

func TestContext_WrongType(t *testing.T) {
	t.Run("GetRegistry_WrongType", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		appCtx.With(&ctxapi.Key{Name: "contracts"}, "not a contractServices")

		reg := GetRegistry(ctx)
		assert.Nil(t, reg)
	})

	t.Run("GetInstantiator_WrongType", func(t *testing.T) {
		appCtx := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), appCtx)

		appCtx.With(&ctxapi.Key{Name: "contracts"}, "not a contractServices")

		inst := GetInstantiator(ctx)
		assert.Nil(t, inst)
	})
}

func TestContext_Idempotent(t *testing.T) {
	ctx := ctxapi.NewRootContext()

	type mockRegistry struct{ Registry }
	mockReg := &mockRegistry{}

	type mockInstantiator struct{ Instantiator }
	mockInst := &mockInstantiator{}

	ctx = WithContracts(ctx, mockReg, mockInst)

	type mockRegistry2 struct{ Registry }
	mockReg2 := &mockRegistry2{}

	type mockInstantiator2 struct{ Instantiator }
	mockInst2 := &mockInstantiator2{}

	WithContracts(ctx, mockReg2, mockInst2)

	assert.Equal(t, mockReg, GetRegistry(ctx))
	assert.Equal(t, mockInst, GetInstantiator(ctx))
}

func TestSentinelErrors(t *testing.T) {
	assert.NotNil(t, ErrInstantiatorNotFound)
	assert.NotNil(t, ErrInstanceNil)
	assert.NotNil(t, ErrNodeNotFound)
	assert.NotNil(t, ErrPIDNotFound)

	assert.Contains(t, ErrInstantiatorNotFound.Error(), "instantiator")
	assert.Contains(t, ErrInstanceNil.Error(), "nil")
	assert.Contains(t, ErrNodeNotFound.Error(), "node")
	assert.Contains(t, ErrPIDNotFound.Error(), "PID")
}

func TestMethodNotBoundError(t *testing.T) {
	err := NewMethodNotBoundError("testMethod")

	assert.Contains(t, err.Error(), "testMethod")
	assert.Contains(t, err.Error(), "not bound")
	assert.NotNil(t, err.Details())
	val, _ := err.Details().Get("method")
	assert.Equal(t, "testMethod", val)
}

func TestMissingContextKeysError(t *testing.T) {
	keys := []string{"key1", "key2", "key3"}
	err := NewMissingContextKeysError(keys)

	assert.Contains(t, err.Error(), "key1")
	assert.Contains(t, err.Error(), "key2")
	assert.Contains(t, err.Error(), "key3")
	assert.Contains(t, err.Error(), "missing required context keys")
	assert.NotNil(t, err.Details())
}

func TestContractNotFoundError(t *testing.T) {
	contractID := registry.NewID("ns", "name")
	err := NewContractNotFoundError(contractID)

	assert.Contains(t, err.Error(), "ns:name")
	assert.Contains(t, err.Error(), "not found")
	assert.NotNil(t, err.Details())
	val, _ := err.Details().Get("contract_id")
	assert.Equal(t, "ns:name", val)
}

func TestBindingNotFoundError(t *testing.T) {
	bindingID := registry.NewID("bindings", "impl")
	err := NewBindingNotFoundError(bindingID)

	assert.Contains(t, err.Error(), "bindings:impl")
	assert.Contains(t, err.Error(), "not found")
	assert.NotNil(t, err.Details())
	val, _ := err.Details().Get("binding_id")
	assert.Equal(t, "bindings:impl", val)
}

func TestNoDefaultBindingError(t *testing.T) {
	contractID := registry.NewID("contracts", "service")
	err := NewNoDefaultBindingError(contractID)

	assert.Contains(t, err.Error(), "contracts:service")
	assert.Contains(t, err.Error(), "no default binding")
	assert.NotNil(t, err.Details())
	val, _ := err.Details().Get("contract_id")
	assert.Equal(t, "contracts:service", val)
}

func TestMethodNotFoundError(t *testing.T) {
	contractID := registry.NewID("contracts", "api")
	err := NewMethodNotFoundError("getData", contractID)

	assert.Contains(t, err.Error(), "getData")
	assert.Contains(t, err.Error(), "contracts:api")
	assert.Contains(t, err.Error(), "not found")
	assert.NotNil(t, err.Details())
	method, _ := err.Details().Get("method")
	assert.Equal(t, "getData", method)
	cid, _ := err.Details().Get("contract_id")
	assert.Equal(t, "contracts:api", cid)
}

func TestErrorInterface(t *testing.T) {
	err := NewContractNotFoundError(registry.NewID("test", "contract"))

	assert.NotEmpty(t, err.Error())
	assert.NotEmpty(t, err.Kind())
	assert.NotNil(t, err.Retryable())
	assert.NotNil(t, err.Details())
}
