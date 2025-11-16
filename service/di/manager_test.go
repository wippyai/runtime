package di

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	apidi "github.com/wippyai/runtime/api/service/di"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// MockPayload implements payload.Payload for testing
type MockPayload struct {
	data   interface{}
	format payload.Format
}

func (p *MockPayload) Data() interface{} {
	return p.data
}

func (p *MockPayload) Format() payload.Format {
	return p.format
}

func (p *MockPayload) Transcode(format payload.Format) (payload.Payload, error) {
	return &MockPayload{data: p.data, format: format}, nil
}

func NewMockPayload(data interface{}) payload.Payload {
	return &MockPayload{data: data, format: payload.Golang}
}

// MockTranscoder implements payload.Transcoder for testing
type MockTranscoder struct {
	unmarshalError error
	unmarshalFunc  func(payload.Payload, interface{}) error
}

func (m *MockTranscoder) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (m *MockTranscoder) Unmarshal(p payload.Payload, v any) error {
	if m.unmarshalError != nil {
		return m.unmarshalError
	}
	if m.unmarshalFunc != nil {
		return m.unmarshalFunc(p, v)
	}

	// Default behavior - copy data from payload
	data := p.Data()
	switch target := v.(type) {
	case *apidi.DefinitionConfig:
		if src, ok := data.(*apidi.DefinitionConfig); ok {
			*target = *src
		}
	case *apidi.BindingConfig:
		if src, ok := data.(*apidi.BindingConfig); ok {
			*target = *src
		}
	}
	return nil
}

func (m *MockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func setupDIManagerTest() (*Manager, event.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	transcoder := &MockTranscoder{}

	manager := NewManager(bus, transcoder, logger)
	return manager, bus
}

func TestNewManager(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	transcoder := &MockTranscoder{}

	manager := NewManager(bus, transcoder, logger)

	assert.NotNil(t, manager)
	assert.Equal(t, logger, manager.log)
	assert.Equal(t, bus, manager.bus)
	assert.Equal(t, transcoder, manager.dtt)
	assert.NotNil(t, manager.definitions)
	assert.NotNil(t, manager.bindings)
}

func TestManager_DefinitionAdd(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := setupDIManagerTest()

	var wg sync.WaitGroup
	var events []event.Event
	var mu sync.Mutex

	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterDefinition, func(evt event.Event) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
		wg.Done()
	})
	require.NoError(t, err)
	defer sub.Close()

	t.Run("successful definition add", func(t *testing.T) {
		events = nil
		entry := registry.Entry{
			Kind: apidi.KindDefinition,
			Data: NewMockPayload(&apidi.DefinitionConfig{
				Methods: []apidi.MethodConfig{
					{
						Name:        "testMethod",
						Description: "Test method",
						InputSchemas: []apidi.SchemaConfig{
							{
								Format:     "application/json",
								Definition: json.RawMessage(`{"type": "object"}`),
							},
						},
						OutputSchemas: []apidi.SchemaConfig{
							{
								Format:     "application/json",
								Definition: json.RawMessage(`{"type": "string"}`),
							},
						},
					},
				},
			}),
		}

		wg.Add(1)
		err := manager.Add(ctx, entry)
		require.NoError(t, err)
		wg.Wait()

		// Verify definition was stored
		manager.mu.RLock()
		_, exists := manager.definitions[entry.ID]
		manager.mu.RUnlock()
		assert.True(t, exists)

		// Verify event was sent
		mu.Lock()
		assert.Len(t, events, 1)
		assert.Equal(t, contract.RegisterDefinition, events[0].Kind)
		mu.Unlock()
	})

	t.Run("wrong entry kind", func(t *testing.T) {
		entry := registry.Entry{
			Kind: "invalid.kind",
			Data: NewMockPayload(&apidi.DefinitionConfig{}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
	})

	t.Run("nil data", func(t *testing.T) {
		entry := registry.Entry{
			Kind: apidi.KindDefinition,
			Data: nil,
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "configuration data is required")
	})

	t.Run("empty method name", func(t *testing.T) {
		entry := registry.Entry{
			Kind: apidi.KindDefinition,
			Data: NewMockPayload(&apidi.DefinitionConfig{
				Methods: []apidi.MethodConfig{
					{Name: ""}, // Empty name
				},
			}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "method name cannot be empty")
	})

	t.Run("duplicate method names", func(t *testing.T) {
		entry := registry.Entry{
			Kind: apidi.KindDefinition,
			Data: NewMockPayload(&apidi.DefinitionConfig{
				Methods: []apidi.MethodConfig{
					{Name: "method1"},
					{Name: "method1"}, // Duplicate
				},
			}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate method name 'method1'")
	})

	t.Run("schema definition without format", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "no_format"},
			Kind: apidi.KindDefinition,
			Data: NewMockPayload(&apidi.DefinitionConfig{
				Methods: []apidi.MethodConfig{
					{
						Name: "method1",
						InputSchemas: []apidi.SchemaConfig{
							{
								Definition: json.RawMessage(`{"type": "object"}`),
								// Missing Format
							},
						},
					},
				},
			}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "input schema 0 for method 'method1' in definition 'test:no_format' has a definition but no format specified")
	})

	t.Run("duplicate definition", func(t *testing.T) {
		entry := registry.Entry{
			Kind: apidi.KindDefinition,
			Data: NewMockPayload(&apidi.DefinitionConfig{}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})
}

func TestManager_DefinitionUpdate(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := setupDIManagerTest()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterDefinition, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer sub.Close()

	updateSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.UpdateDefinition, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer updateSub.Close()

	// First add a definition
	defID := registry.ID{NS: "test", Name: "def1"}
	addEntry := registry.Entry{
		ID:   defID,
		Kind: apidi.KindDefinition,
		Data: NewMockPayload(&apidi.DefinitionConfig{
			Methods: []apidi.MethodConfig{
				{Name: "method1"},
			},
		}),
	}

	wg.Add(1)
	err = manager.Add(ctx, addEntry)
	require.NoError(t, err)
	wg.Wait()

	t.Run("successful update", func(t *testing.T) {
		updateEntry := registry.Entry{
			ID:   defID,
			Kind: apidi.KindDefinition,
			Data: NewMockPayload(&apidi.DefinitionConfig{
				Methods: []apidi.MethodConfig{
					{Name: "method1"},
					{Name: "method2"},
				},
			}),
		}

		wg.Add(1)
		err := manager.Update(ctx, updateEntry)
		require.NoError(t, err)
		wg.Wait()
	})

	t.Run("definition not found", func(t *testing.T) {
		updateEntry := registry.Entry{
			Kind: apidi.KindDefinition,
			Data: NewMockPayload(&apidi.DefinitionConfig{}),
		}

		err := manager.Update(ctx, updateEntry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found for update")
	})
}

func TestManager_DefinitionDelete(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := setupDIManagerTest()

	var wg sync.WaitGroup
	addSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterDefinition, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer addSub.Close()

	deleteSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.DeleteDefinition, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer deleteSub.Close()

	// Add a definition first
	defID := registry.ID{NS: "test", Name: "def1"}
	addEntry := registry.Entry{
		ID:   defID,
		Kind: apidi.KindDefinition,
		Data: NewMockPayload(&apidi.DefinitionConfig{
			Methods: []apidi.MethodConfig{{Name: "method1"}},
		}),
	}

	wg.Add(1)
	err = manager.Add(ctx, addEntry)
	require.NoError(t, err)
	wg.Wait()

	t.Run("successful delete", func(t *testing.T) {
		wg.Add(1)
		err := manager.Delete(ctx, addEntry)
		require.NoError(t, err)
		wg.Wait()

		// Verify definition was removed
		manager.mu.RLock()
		_, exists := manager.definitions[defID]
		manager.mu.RUnlock()
		assert.False(t, exists)
	})

	t.Run("definition not found", func(t *testing.T) {
		err := manager.Delete(ctx, addEntry) // Try to delete again
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found for deletion")
	})
}

func TestManager_BindingOperations(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := setupDIManagerTest()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterDefinition, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer sub.Close()

	bindingSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterBinding, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer bindingSub.Close()

	// First add a definition that bindings can reference
	defID := registry.ID{NS: "test", Name: "def1"}
	defEntry := registry.Entry{
		ID:   defID,
		Kind: apidi.KindDefinition,
		Data: NewMockPayload(&apidi.DefinitionConfig{
			Methods: []apidi.MethodConfig{
				{Name: "method1"},
				{Name: "method2"},
			},
		}),
	}

	wg.Add(1)
	err = manager.Add(ctx, defEntry)
	require.NoError(t, err)
	wg.Wait()

	t.Run("successful binding add", func(t *testing.T) {
		entry := registry.Entry{
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: defID.String(),
						Methods: map[string]string{
							"method1": "test:func1",
							"method2": "test:func2",
						},
					},
				},
			}),
		}

		wg.Add(1)
		err := manager.Add(ctx, entry)
		require.NoError(t, err)
		wg.Wait()

		// Verify binding was stored
		manager.mu.RLock()
		_, exists := manager.bindings[entry.ID]
		manager.mu.RUnlock()
		assert.True(t, exists)
	})

	t.Run("binding validation errors", func(t *testing.T) {
		tests := []struct {
			name        string
			entry       registry.Entry
			expectError string
		}{
			{
				name: "wrong entry kind",
				entry: registry.Entry{
					Kind: "invalid.kind",
					Data: NewMockPayload(&apidi.BindingConfig{}),
				},
				expectError: "unsupported entry kind",
			},
			{
				name: "nil data",
				entry: registry.Entry{
					ID:   registry.ID{NS: "test", Name: "binding-nil"},
					Kind: apidi.KindBinding,
					Data: nil,
				},
				expectError: "configuration data is required",
			},
			{
				name: "empty contracts",
				entry: registry.Entry{
					ID:   registry.ID{NS: "test", Name: "binding-empty"},
					Kind: apidi.KindBinding,
					Data: NewMockPayload(&apidi.BindingConfig{
						Contracts: []apidi.BoundContractConfig{},
					}),
				},
				expectError: "must bind at least one contract",
			},
			{
				name: "contract not found",
				entry: registry.Entry{
					ID:   registry.ID{NS: "test", Name: "binding-notfound"},
					Kind: apidi.KindBinding,
					Data: NewMockPayload(&apidi.BindingConfig{
						Contracts: []apidi.BoundContractConfig{
							{
								Contract: "test:missing",
								Methods:  map[string]string{"method1": "test:func1"},
							},
						},
					}),
				},
				expectError: "contract definition not found",
			},
			{
				name: "missing method in binding",
				entry: registry.Entry{
					ID:   registry.ID{NS: "test", Name: "binding-missing-method"},
					Kind: apidi.KindBinding,
					Data: NewMockPayload(&apidi.BindingConfig{
						Contracts: []apidi.BoundContractConfig{
							{
								Contract: defID.String(),
								Methods: map[string]string{
									"method1": "test:func1",
									// Missing method2
								},
							},
						},
					}),
				},
				expectError: "method 'method2' defined in contract is not bound",
			},
			{
				name: "extra method in binding",
				entry: registry.Entry{
					ID:   registry.ID{NS: "test", Name: "binding-extra-method"},
					Kind: apidi.KindBinding,
					Data: NewMockPayload(&apidi.BindingConfig{
						Contracts: []apidi.BoundContractConfig{
							{
								Contract: defID.String(),
								Methods: map[string]string{
									"method1":   "test:func1",
									"method2":   "test:func2",
									"method999": "test:func999", // Extra
								},
							},
						},
					}),
				},
				expectError: "bound method 'method999' is not defined in contract definition",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := manager.Add(ctx, tt.entry)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectError)
			})
		}
	})
}

func TestManager_DefaultBindingValidation(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := setupDIManagerTest()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterDefinition, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer sub.Close()

	bindingSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterBinding, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer bindingSub.Close()

	// Add contract definitions
	defID1 := registry.ID{NS: "test", Name: "contract1"}
	defID2 := registry.ID{NS: "test", Name: "contract2"}

	defEntry1 := registry.Entry{
		ID:   defID1,
		Kind: apidi.KindDefinition,
		Data: NewMockPayload(&apidi.DefinitionConfig{
			Methods: []apidi.MethodConfig{{Name: "method1"}},
		}),
	}

	defEntry2 := registry.Entry{
		ID:   defID2,
		Kind: apidi.KindDefinition,
		Data: NewMockPayload(&apidi.DefinitionConfig{
			Methods: []apidi.MethodConfig{{Name: "method2"}},
		}),
	}

	wg.Add(2)
	err = manager.Add(ctx, defEntry1)
	require.NoError(t, err)
	err = manager.Add(ctx, defEntry2)
	require.NoError(t, err)
	wg.Wait()

	t.Run("successful default binding add", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "binding1"},
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: defID1.String(),
						Methods:  map[string]string{"method1": "test:func1"},
						Default:  true,
					},
				},
			}),
		}

		wg.Add(1)
		err := manager.Add(ctx, entry)
		require.NoError(t, err)
		wg.Wait()

		// Verify binding was stored
		manager.mu.RLock()
		binding, exists := manager.bindings[entry.ID]
		manager.mu.RUnlock()
		assert.True(t, exists)
		assert.True(t, binding.Contracts[0].Default)
	})

	t.Run("duplicate default binding for same contract", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "binding2"},
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: defID1.String(), // Same contract as above
						Methods:  map[string]string{"method1": "test:func2"},
						Default:  true, // Trying to set another default
					},
				},
			}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already has default binding")
		assert.Contains(t, err.Error(), "cannot set binding")
		assert.Contains(t, err.Error(), "as default")
	})

	t.Run("different contracts can have defaults", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "binding3"},
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: defID2.String(), // Different contract
						Methods:  map[string]string{"method2": "test:func3"},
						Default:  true,
					},
				},
			}),
		}

		wg.Add(1)
		err := manager.Add(ctx, entry)
		require.NoError(t, err)
		wg.Wait()

		// Verify binding was stored
		manager.mu.RLock()
		binding, exists := manager.bindings[entry.ID]
		manager.mu.RUnlock()
		assert.True(t, exists)
		assert.True(t, binding.Contracts[0].Default)
	})

	t.Run("non-default bindings are allowed", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "binding4"},
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: defID1.String(),
						Methods:  map[string]string{"method1": "test:func4"},
						Default:  false, // Explicitly non-default
					},
				},
			}),
		}

		wg.Add(1)
		err := manager.Add(ctx, entry)
		require.NoError(t, err)
		wg.Wait()

		// Verify binding was stored
		manager.mu.RLock()
		binding, exists := manager.bindings[entry.ID]
		manager.mu.RUnlock()
		assert.True(t, exists)
		assert.False(t, binding.Contracts[0].Default)
	})

	t.Run("update existing binding to set default on occupied contract", func(t *testing.T) {
		// First create a non-default binding
		bindingID := registry.ID{NS: "test", Name: "binding5"}
		entry := registry.Entry{
			ID:   bindingID,
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: defID1.String(),
						Methods:  map[string]string{"method1": "test:func5"},
						Default:  false,
					},
				},
			}),
		}

		wg.Add(1)
		err := manager.Add(ctx, entry)
		require.NoError(t, err)
		wg.Wait()

		// Now try to update it to be default (should fail because defID1 already has a default)
		updateEntry := registry.Entry{
			ID:   bindingID,
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: defID1.String(),
						Methods:  map[string]string{"method1": "test:func5"},
						Default:  true, // Trying to set as default
					},
				},
			}),
		}

		err = manager.Update(ctx, updateEntry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already has default binding")
	})
}

func TestManager_ValidationEdgeCases(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := setupDIManagerTest()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterDefinition, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer sub.Close()

	bindingSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterBinding, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer bindingSub.Close()

	// Add definition and binding for dependency tests
	defID := registry.ID{NS: "test", Name: "def1"}
	defEntry := registry.Entry{
		ID:   defID,
		Kind: apidi.KindDefinition,
		Data: NewMockPayload(&apidi.DefinitionConfig{
			Methods: []apidi.MethodConfig{{Name: "method1"}},
		}),
	}

	bindingEntry := registry.Entry{
		Kind: apidi.KindBinding,
		Data: NewMockPayload(&apidi.BindingConfig{
			Contracts: []apidi.BoundContractConfig{
				{
					Contract: defID.String(),
					Methods:  map[string]string{"method1": "test:func1"},
				},
			},
		}),
	}

	wg.Add(2)
	err = manager.Add(ctx, defEntry)
	require.NoError(t, err)
	err = manager.Add(ctx, bindingEntry)
	require.NoError(t, err)
	wg.Wait()

	t.Run("cannot delete definition used by binding", func(t *testing.T) {
		err := manager.Delete(ctx, defEntry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot delete contract definition")
		assert.Contains(t, err.Error(), "it is used by binding")
	})

	t.Run("definition update invalidates bindings", func(t *testing.T) {
		// Try to update definition to remove method1 - should fail
		updatedDefEntry := registry.Entry{
			ID:   defID,
			Kind: apidi.KindDefinition,
			Data: NewMockPayload(&apidi.DefinitionConfig{
				Methods: []apidi.MethodConfig{{Name: "method2"}}, // Different method
			}),
		}

		err := manager.Update(ctx, updatedDefEntry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "updating definition")
		assert.Contains(t, err.Error(), "would invalidate binding")
	})
}

func TestManager_UnmarshalError(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, _ := setupDIManagerTest()

	// Configure transcoder to return error
	manager.dtt = &MockTranscoder{unmarshalError: fmt.Errorf("unmarshal failed")}

	entry := registry.Entry{
		Kind: apidi.KindDefinition,
		Data: NewMockPayload("invalid data"),
	}

	err := manager.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode definition")
}

// TestManager_DefaultBindingValidationEdgeCases tests more complex scenarios
func TestManager_DefaultBindingValidationEdgeCases(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := setupDIManagerTest()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterDefinition, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer sub.Close()

	bindingSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterBinding, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer bindingSub.Close()

	// Add multiple contract definitions
	contractIDs := []registry.ID{
		{NS: "test", Name: "contract1"},
		{NS: "test", Name: "contract2"},
		{NS: "test", Name: "contract3"},
	}

	for i, contractID := range contractIDs {
		defEntry := registry.Entry{
			ID:   contractID,
			Kind: apidi.KindDefinition,
			Data: NewMockPayload(&apidi.DefinitionConfig{
				Methods: []apidi.MethodConfig{{Name: fmt.Sprintf("method%d", i+1)}},
			}),
		}
		wg.Add(1)
		err = manager.Add(ctx, defEntry)
		require.NoError(t, err)
	}
	wg.Wait()

	t.Run("multi-contract binding with mixed defaults", func(t *testing.T) {
		// Binding that implements multiple contracts with different default settings
		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "multi_contract_binding"},
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: contractIDs[0].String(),
						Methods:  map[string]string{"method1": "test:func1"},
						Default:  true, // Default for contract1
					},
					{
						Contract: contractIDs[1].String(),
						Methods:  map[string]string{"method2": "test:func2"},
						Default:  false, // Not default for contract2
					},
					{
						Contract: contractIDs[2].String(),
						Methods:  map[string]string{"method3": "test:func3"},
						Default:  true, // Default for contract3
					},
				},
			}),
		}

		wg.Add(1)
		err := manager.Add(ctx, entry)
		require.NoError(t, err)
		wg.Wait()

		// Verify binding was stored with correct default settings
		manager.mu.RLock()
		binding, exists := manager.bindings[entry.ID]
		manager.mu.RUnlock()
		assert.True(t, exists)
		assert.True(t, binding.Contracts[0].Default)  // contract1
		assert.False(t, binding.Contracts[1].Default) // contract2
		assert.True(t, binding.Contracts[2].Default)  // contract3
	})

	t.Run("attempt to add conflicting default for contract1", func(t *testing.T) {
		// Try to add another binding with default=true for contract1 (should fail)
		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "conflicting_default"},
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: contractIDs[0].String(), // Same as above
						Methods:  map[string]string{"method1": "test:func_other"},
						Default:  true, // Conflict!
					},
				},
			}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), fmt.Sprintf("contract '%s' already has default binding", contractIDs[0]))
		assert.Contains(t, err.Error(), "multi_contract_binding")
		assert.Contains(t, err.Error(), "cannot set binding 'test:conflicting_default' as default")
	})

	t.Run("attempt to add conflicting default for contract3", func(t *testing.T) {
		// Try to add another binding with default=true for contract3 (should fail)
		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "conflicting_default3"},
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: contractIDs[2].String(), // contract3, already has default
						Methods:  map[string]string{"method3": "test:func_other"},
						Default:  true, // Conflict!
					},
				},
			}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), fmt.Sprintf("contract '%s' already has default binding", contractIDs[2]))
	})

	t.Run("can add default for contract2 (no existing default)", func(t *testing.T) {
		// contract2 doesn't have a default yet, so this should work
		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "contract2_default"},
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: contractIDs[1].String(), // contract2
						Methods:  map[string]string{"method2": "test:func_default"},
						Default:  true, // Should work
					},
				},
			}),
		}

		wg.Add(1)
		err := manager.Add(ctx, entry)
		require.NoError(t, err)
		wg.Wait()

		// Verify binding was stored
		manager.mu.RLock()
		binding, exists := manager.bindings[entry.ID]
		manager.mu.RUnlock()
		assert.True(t, exists)
		assert.True(t, binding.Contracts[0].Default)
	})

	t.Run("can add non-default bindings for contracts with existing defaults", func(t *testing.T) {
		// Should be able to add non-default bindings even when defaults exist
		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "non_default_binding"},
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: contractIDs[0].String(), // Has existing default
						Methods:  map[string]string{"method1": "test:func_alt1"},
						Default:  false, // Non-default, should be fine
					},
					{
						Contract: contractIDs[1].String(), // Has existing default
						Methods:  map[string]string{"method2": "test:func_alt2"},
						Default:  false, // Non-default, should be fine
					},
				},
			}),
		}

		wg.Add(1)
		err := manager.Add(ctx, entry)
		require.NoError(t, err)
		wg.Wait()

		// Verify binding was stored
		manager.mu.RLock()
		binding, exists := manager.bindings[entry.ID]
		manager.mu.RUnlock()
		assert.True(t, exists)
		assert.False(t, binding.Contracts[0].Default)
		assert.False(t, binding.Contracts[1].Default)
	})
}

// TestManager_DefaultBindingUpdateScenarios tests complex update scenarios
func TestManager_DefaultBindingUpdateScenarios(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := setupDIManagerTest()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterDefinition, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer sub.Close()

	bindingSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterBinding, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer bindingSub.Close()

	updateSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.UpdateBinding, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer updateSub.Close()

	// Setup contracts
	contractIDs := []registry.ID{
		{NS: "test", Name: "service1"},
		{NS: "test", Name: "service2"},
	}

	for i, contractID := range contractIDs {
		defEntry := registry.Entry{
			ID:   contractID,
			Kind: apidi.KindDefinition,
			Data: NewMockPayload(&apidi.DefinitionConfig{
				Methods: []apidi.MethodConfig{{Name: fmt.Sprintf("serve%d", i+1)}},
			}),
		}
		wg.Add(1)
		err = manager.Add(ctx, defEntry)
		require.NoError(t, err)
	}
	wg.Wait()

	// Create initial bindings
	bindingAID := registry.ID{NS: "test", Name: "bindingA"}
	bindingBID := registry.ID{NS: "test", Name: "bindingB"}

	// BindingA: default for service1, non-default for service2
	entryA := registry.Entry{
		ID:   bindingAID,
		Kind: apidi.KindBinding,
		Data: NewMockPayload(&apidi.BindingConfig{
			Contracts: []apidi.BoundContractConfig{
				{
					Contract: contractIDs[0].String(),
					Methods:  map[string]string{"serve1": "test:funcA1"},
					Default:  true,
				},
				{
					Contract: contractIDs[1].String(),
					Methods:  map[string]string{"serve2": "test:funcA2"},
					Default:  false,
				},
			},
		}),
	}

	// BindingB: non-default for service1, default for service2
	entryB := registry.Entry{
		ID:   bindingBID,
		Kind: apidi.KindBinding,
		Data: NewMockPayload(&apidi.BindingConfig{
			Contracts: []apidi.BoundContractConfig{
				{
					Contract: contractIDs[0].String(),
					Methods:  map[string]string{"serve1": "test:funcB1"},
					Default:  false,
				},
				{
					Contract: contractIDs[1].String(),
					Methods:  map[string]string{"serve2": "test:funcB2"},
					Default:  true,
				},
			},
		}),
	}

	wg.Add(2)
	err = manager.Add(ctx, entryA)
	require.NoError(t, err)
	err = manager.Add(ctx, entryB)
	require.NoError(t, err)
	wg.Wait()

	t.Run("attempt to flip defaults - should fail", func(t *testing.T) {
		// Try to update bindingA to make it default for service2 (conflicts with bindingB)
		updateEntry := registry.Entry{
			ID:   bindingAID,
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: contractIDs[0].String(),
						Methods:  map[string]string{"serve1": "test:funcA1"},
						Default:  true, // Keep existing default
					},
					{
						Contract: contractIDs[1].String(),
						Methods:  map[string]string{"serve2": "test:funcA2"},
						Default:  true, // Try to become default (conflicts with bindingB)
					},
				},
			}),
		}

		err := manager.Update(ctx, updateEntry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), fmt.Sprintf("contract '%s' already has default binding", contractIDs[1]))
		assert.Contains(t, err.Error(), "test:bindingB")
	})

	t.Run("remove default from bindingA for service1", func(t *testing.T) {
		// Update bindingA to remove its default status for service1
		updateEntry := registry.Entry{
			ID:   bindingAID,
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: contractIDs[0].String(),
						Methods:  map[string]string{"serve1": "test:funcA1"},
						Default:  false, // Remove default
					},
					{
						Contract: contractIDs[1].String(),
						Methods:  map[string]string{"serve2": "test:funcA2"},
						Default:  false, // Keep non-default
					},
				},
			}),
		}

		wg.Add(1)
		err := manager.Update(ctx, updateEntry)
		require.NoError(t, err)
		wg.Wait()

		// Verify the update
		manager.mu.RLock()
		binding, exists := manager.bindings[bindingAID]
		manager.mu.RUnlock()
		assert.True(t, exists)
		assert.False(t, binding.Contracts[0].Default) // No longer default for service1
		assert.False(t, binding.Contracts[1].Default) // Still non-default for service2
	})

	t.Run("now bindingB can become default for service1", func(t *testing.T) {
		// Now that bindingA is no longer default for service1, bindingB can become default
		updateEntry := registry.Entry{
			ID:   bindingBID,
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: contractIDs[0].String(),
						Methods:  map[string]string{"serve1": "test:funcB1"},
						Default:  true, // Now can become default
					},
					{
						Contract: contractIDs[1].String(),
						Methods:  map[string]string{"serve2": "test:funcB2"},
						Default:  true, // Keep existing default
					},
				},
			}),
		}

		wg.Add(1)
		err := manager.Update(ctx, updateEntry)
		require.NoError(t, err)
		wg.Wait()

		// Verify bindingB is now default for both services
		manager.mu.RLock()
		binding, exists := manager.bindings[bindingBID]
		manager.mu.RUnlock()
		assert.True(t, exists)
		assert.True(t, binding.Contracts[0].Default) // Now default for service1
		assert.True(t, binding.Contracts[1].Default) // Still default for service2
	})

	t.Run("bindingA can now become default for service1 again", func(t *testing.T) {
		// First, remove bindingB's default for service1
		updateEntryB := registry.Entry{
			ID:   bindingBID,
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: contractIDs[0].String(),
						Methods:  map[string]string{"serve1": "test:funcB1"},
						Default:  false, // Remove default
					},
					{
						Contract: contractIDs[1].String(),
						Methods:  map[string]string{"serve2": "test:funcB2"},
						Default:  true, // Keep default
					},
				},
			}),
		}

		wg.Add(1)
		err := manager.Update(ctx, updateEntryB)
		require.NoError(t, err)
		wg.Wait()

		// Now bindingA can become default for service1
		updateEntryA := registry.Entry{
			ID:   bindingAID,
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: contractIDs[0].String(),
						Methods:  map[string]string{"serve1": "test:funcA1"},
						Default:  true, // Can become default again
					},
					{
						Contract: contractIDs[1].String(),
						Methods:  map[string]string{"serve2": "test:funcA2"},
						Default:  false, // Stay non-default
					},
				},
			}),
		}

		wg.Add(1)
		err = manager.Update(ctx, updateEntryA)
		require.NoError(t, err)
		wg.Wait()

		// Verify final state
		manager.mu.RLock()
		bindingA, existsA := manager.bindings[bindingAID]
		bindingB, existsB := manager.bindings[bindingBID]
		manager.mu.RUnlock()

		assert.True(t, existsA)
		assert.True(t, existsB)

		// bindingA: default for service1, non-default for service2
		assert.True(t, bindingA.Contracts[0].Default)
		assert.False(t, bindingA.Contracts[1].Default)

		// bindingB: non-default for service1, default for service2
		assert.False(t, bindingB.Contracts[0].Default)
		assert.True(t, bindingB.Contracts[1].Default)
	})
}

// TestManager_DefaultBindingDeleteScenarios tests deletion scenarios with default bindings
func TestManager_DefaultBindingDeleteScenarios(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := setupDIManagerTest()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterDefinition, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer sub.Close()

	bindingSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterBinding, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer bindingSub.Close()

	deleteSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.DeleteBinding, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer deleteSub.Close()

	// Setup contract
	contractID := registry.ID{NS: "test", Name: "delete_test_contract"}
	defEntry := registry.Entry{
		ID:   contractID,
		Kind: apidi.KindDefinition,
		Data: NewMockPayload(&apidi.DefinitionConfig{
			Methods: []apidi.MethodConfig{{Name: "execute"}},
		}),
	}

	wg.Add(1)
	err = manager.Add(ctx, defEntry)
	require.NoError(t, err)
	wg.Wait()

	// Add default binding
	defaultBindingID := registry.ID{NS: "test", Name: "default_impl"}
	defaultEntry := registry.Entry{
		ID:   defaultBindingID,
		Kind: apidi.KindBinding,
		Data: NewMockPayload(&apidi.BindingConfig{
			Contracts: []apidi.BoundContractConfig{
				{
					Contract: contractID.String(),
					Methods:  map[string]string{"execute": "test:default_func"},
					Default:  true,
				},
			},
		}),
	}

	wg.Add(1)
	err = manager.Add(ctx, defaultEntry)
	require.NoError(t, err)
	wg.Wait()

	// Add non-default binding
	altBindingID := registry.ID{NS: "test", Name: "alt_impl"}
	altEntry := registry.Entry{
		ID:   altBindingID,
		Kind: apidi.KindBinding,
		Data: NewMockPayload(&apidi.BindingConfig{
			Contracts: []apidi.BoundContractConfig{
				{
					Contract: contractID.String(),
					Methods:  map[string]string{"execute": "test:alt_func"},
					Default:  false,
				},
			},
		}),
	}

	wg.Add(1)
	err = manager.Add(ctx, altEntry)
	require.NoError(t, err)
	wg.Wait()

	t.Run("delete non-default binding - should work", func(t *testing.T) {
		wg.Add(1)
		err := manager.Delete(ctx, altEntry)
		require.NoError(t, err)
		wg.Wait()

		// Verify alt binding is gone but default remains
		manager.mu.RLock()
		_, altExists := manager.bindings[altBindingID]
		defaultBinding, defaultExists := manager.bindings[defaultBindingID]
		manager.mu.RUnlock()

		assert.False(t, altExists)
		assert.True(t, defaultExists)
		assert.True(t, defaultBinding.Contracts[0].Default)
	})

	t.Run("delete default binding - should work", func(t *testing.T) {
		wg.Add(1)
		err := manager.Delete(ctx, defaultEntry)
		require.NoError(t, err)
		wg.Wait()

		// Verify default binding is gone
		manager.mu.RLock()
		_, defaultExists := manager.bindings[defaultBindingID]
		manager.mu.RUnlock()

		assert.False(t, defaultExists)
	})

	t.Run("can add new default after deletion", func(t *testing.T) {
		// After deleting the default binding, we should be able to add a new default
		newDefaultID := registry.ID{NS: "test", Name: "new_default_impl"}
		newDefaultEntry := registry.Entry{
			ID:   newDefaultID,
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: contractID.String(),
						Methods:  map[string]string{"execute": "test:new_default_func"},
						Default:  true,
					},
				},
			}),
		}

		wg.Add(1)
		err := manager.Add(ctx, newDefaultEntry)
		require.NoError(t, err)
		wg.Wait()

		// Verify new default is stored
		manager.mu.RLock()
		newBinding, exists := manager.bindings[newDefaultID]
		manager.mu.RUnlock()

		assert.True(t, exists)
		assert.True(t, newBinding.Contracts[0].Default)
	})
}

// TestManager_DefaultBindingErrorMessages tests that error messages are descriptive
func TestManager_DefaultBindingErrorMessages(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := setupDIManagerTest()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterDefinition, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer sub.Close()

	bindingSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterBinding, func(_ event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer bindingSub.Close()

	// Setup
	contractID := registry.ID{NS: "service", Name: "payment_processor"}
	defEntry := registry.Entry{
		ID:   contractID,
		Kind: apidi.KindDefinition,
		Data: NewMockPayload(&apidi.DefinitionConfig{
			Methods: []apidi.MethodConfig{{Name: "process_payment"}},
		}),
	}

	wg.Add(1)
	err = manager.Add(ctx, defEntry)
	require.NoError(t, err)
	wg.Wait()

	// Add first default binding
	firstDefaultID := registry.ID{NS: "impl", Name: "stripe_payment"}
	firstEntry := registry.Entry{
		ID:   firstDefaultID,
		Kind: apidi.KindBinding,
		Data: NewMockPayload(&apidi.BindingConfig{
			Contracts: []apidi.BoundContractConfig{
				{
					Contract: contractID.String(),
					Methods:  map[string]string{"process_payment": "stripe:process"},
					Default:  true,
				},
			},
		}),
	}

	wg.Add(1)
	err = manager.Add(ctx, firstEntry)
	require.NoError(t, err)
	wg.Wait()

	t.Run("error message contains all relevant information", func(t *testing.T) {
		// Try to add conflicting default
		secondDefaultID := registry.ID{NS: "impl", Name: "paypal_payment"}
		secondEntry := registry.Entry{
			ID:   secondDefaultID,
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: contractID.String(),
						Methods:  map[string]string{"process_payment": "paypal:process"},
						Default:  true,
					},
				},
			}),
		}

		err := manager.Add(ctx, secondEntry)
		require.Error(t, err)

		errorMsg := err.Error()

		// Error should contain:
		// 1. The contract ID that has the conflict
		assert.Contains(t, errorMsg, contractID.String())

		// 2. The existing default binding ID
		assert.Contains(t, errorMsg, firstDefaultID.String())

		// 3. The new binding ID being rejected
		assert.Contains(t, errorMsg, secondDefaultID.String())

		// 4. Clear indication of what went wrong
		assert.Contains(t, errorMsg, "already has default binding")
		assert.Contains(t, errorMsg, "cannot set binding")
		assert.Contains(t, errorMsg, "as default")

		// Full expected pattern
		expectedPattern := fmt.Sprintf("contract '%s' already has default binding '%s', cannot set binding '%s' as default",
			contractID.String(), firstDefaultID.String(), secondDefaultID.String())
		assert.Equal(t, expectedPattern, errorMsg)
	})

	t.Run("update error message is equally descriptive", func(t *testing.T) {
		// Add a non-default binding first
		nonDefaultID := registry.ID{NS: "impl", Name: "square_payment"}
		nonDefaultEntry := registry.Entry{
			ID:   nonDefaultID,
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: contractID.String(),
						Methods:  map[string]string{"process_payment": "square:process"},
						Default:  false,
					},
				},
			}),
		}

		wg.Add(1)
		err = manager.Add(ctx, nonDefaultEntry)
		require.NoError(t, err)
		wg.Wait()

		// Now try to update it to be default (should conflict)
		updateEntry := registry.Entry{
			ID:   nonDefaultID,
			Kind: apidi.KindBinding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: contractID.String(),
						Methods:  map[string]string{"process_payment": "square:process"},
						Default:  true, // Try to become default
					},
				},
			}),
		}

		err = manager.Update(ctx, updateEntry)
		require.Error(t, err)

		errorMsg := err.Error()
		assert.Contains(t, errorMsg, contractID.String())
		assert.Contains(t, errorMsg, firstDefaultID.String()) // Existing default
		assert.Contains(t, errorMsg, nonDefaultID.String())   // Binding being updated
		assert.Contains(t, errorMsg, "already has default binding")
		assert.Contains(t, errorMsg, "cannot set binding")
		assert.Contains(t, errorMsg, "as default")
	})
}
