// SPDX-License-Identifier: MPL-2.0

package di

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/contract"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	apidi "github.com/wippyai/runtime/api/service/di"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// MockPayload implements payload.Payload for testing
type MockPayload struct {
	data   any
	format payload.Format
}

func (p *MockPayload) Data() any {
	return p.data
}

func (p *MockPayload) Format() payload.Format {
	return p.format
}

func (p *MockPayload) Transcode(format payload.Format) (payload.Payload, error) {
	return &MockPayload{data: p.data, format: format}, nil
}

func NewMockPayload(data any) payload.Payload {
	return &MockPayload{data: data, format: payload.Golang}
}

func requireAPIError(t *testing.T, err error, kind apierror.Kind, msg string) apierror.Error {
	t.Helper()
	require.Error(t, err)
	var apiErr apierror.Error
	ok := errors.As(err, &apiErr)
	require.Truef(t, ok, "expected apierror.Error, got %T", err)
	assert.Equal(t, kind, apiErr.Kind())
	assert.Contains(t, err.Error(), msg)
	return apiErr
}

func assertDetailString(t *testing.T, apiErr apierror.Error, key, expected string) {
	t.Helper()
	assert.Equal(t, expected, apiErr.Details().GetString(key, ""))
}

func assertDetailInt(t *testing.T, apiErr apierror.Error, key string, expected int) {
	t.Helper()
	assert.Equal(t, expected, apiErr.Details().GetInt(key, 0))
}

// MockTranscoder implements payload.Transcoder for testing
type MockTranscoder struct {
	unmarshalError error
	unmarshalFunc  func(payload.Payload, any) error
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
			ID:   registry.NewID("test", "def1"),
			Kind: apidi.Definition,
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
		apiErr := requireAPIError(t, err, apierror.Invalid, "unsupported entry kind")
		assertDetailString(t, apiErr, "kind", "invalid.kind")
	})

	t.Run("nil data", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.NewID("test", "nil_definition"),
			Kind: apidi.Definition,
			Data: nil,
		}

		err := manager.Add(ctx, entry)
		apiErr := requireAPIError(t, err, apierror.Invalid, "failed to decode definition")
		assertDetailString(t, apiErr, "definition_id", entry.ID.String())
		assert.NotEmpty(t, apiErr.Details().GetString("cause", ""))
	})

	t.Run("empty method name", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.NewID("test", "empty_method"),
			Kind: apidi.Definition,
			Data: NewMockPayload(&apidi.DefinitionConfig{
				Methods: []apidi.MethodConfig{
					{Name: ""}, // Empty name
				},
			}),
		}

		err := manager.Add(ctx, entry)
		apiErr := requireAPIError(t, err, apierror.Invalid, "method name cannot be empty")
		assertDetailString(t, apiErr, "definition_id", entry.ID.String())
	})

	t.Run("duplicate method names", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.NewID("test", "duplicate_methods"),
			Kind: apidi.Definition,
			Data: NewMockPayload(&apidi.DefinitionConfig{
				Methods: []apidi.MethodConfig{
					{Name: "method1"},
					{Name: "method1"}, // Duplicate
				},
			}),
		}

		err := manager.Add(ctx, entry)
		apiErr := requireAPIError(t, err, apierror.Invalid, "duplicate method name")
		assertDetailString(t, apiErr, "definition_id", entry.ID.String())
		assertDetailString(t, apiErr, "method_name", "method1")
	})

	t.Run("schema definition without format", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.NewID("test", "no_format"),
			Kind: apidi.Definition,
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
		apiErr := requireAPIError(t, err, apierror.Invalid, "input schema has a definition but no format specified")
		assertDetailString(t, apiErr, "definition_id", entry.ID.String())
		assertDetailString(t, apiErr, "method_name", "method1")
		assertDetailInt(t, apiErr, "schema_index", 0)
	})

	t.Run("duplicate definition", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.NewID("test", "def1"),
			Kind: apidi.Definition,
			Data: NewMockPayload(&apidi.DefinitionConfig{}),
		}

		err := manager.Add(ctx, entry)
		apiErr := requireAPIError(t, err, apierror.AlreadyExists, "contract definition already exists")
		assertDetailString(t, apiErr, "definition_id", entry.ID.String())
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
	defID := registry.NewID("test", "def1")
	addEntry := registry.Entry{
		ID:   defID,
		Kind: apidi.Definition,
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
			Kind: apidi.Definition,
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
			ID:   registry.NewID("test", "missing_def"),
			Kind: apidi.Definition,
			Data: NewMockPayload(&apidi.DefinitionConfig{}),
		}

		err := manager.Update(ctx, updateEntry)
		apiErr := requireAPIError(t, err, apierror.NotFound, "contract definition not found for update")
		assertDetailString(t, apiErr, "definition_id", updateEntry.ID.String())
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
	defID := registry.NewID("test", "def1")
	addEntry := registry.Entry{
		ID:   defID,
		Kind: apidi.Definition,
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
		apiErr := requireAPIError(t, err, apierror.NotFound, "contract definition not found for deletion")
		assertDetailString(t, apiErr, "definition_id", addEntry.ID.String())
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
	defID := registry.NewID("test", "def1")
	defEntry := registry.Entry{
		ID:   defID,
		Kind: apidi.Definition,
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
			Kind: apidi.Binding,
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
			entry         registry.Entry
			assertDetails func(*testing.T, apierror.Error)
			name          string
			expectedKind  apierror.Kind
			expectedMsg   string
		}{
			{
				name: "wrong entry kind",
				entry: registry.Entry{
					Kind: "invalid.kind",
					Data: NewMockPayload(&apidi.BindingConfig{}),
				},
				expectedKind: apierror.Invalid,
				expectedMsg:  "unsupported entry kind",
				assertDetails: func(t *testing.T, apiErr apierror.Error) {
					assertDetailString(t, apiErr, "kind", "invalid.kind")
				},
			},
			{
				name: "nil data",
				entry: registry.Entry{
					ID:   registry.NewID("test", "binding-nil"),
					Kind: apidi.Binding,
					Data: nil,
				},
				expectedKind: apierror.Invalid,
				expectedMsg:  "failed to decode binding",
				assertDetails: func(t *testing.T, apiErr apierror.Error) {
					assertDetailString(t, apiErr, "binding_id", "test:binding-nil")
					assert.NotEmpty(t, apiErr.Details().GetString("cause", ""))
				},
			},
			{
				name: "empty contracts",
				entry: registry.Entry{
					ID:   registry.NewID("test", "binding-empty"),
					Kind: apidi.Binding,
					Data: NewMockPayload(&apidi.BindingConfig{
						Contracts: []apidi.BoundContractConfig{},
					}),
				},
				expectedKind: apierror.Invalid,
				expectedMsg:  "binding must bind at least one contract",
				assertDetails: func(t *testing.T, apiErr apierror.Error) {
					assertDetailString(t, apiErr, "binding_id", "test:binding-empty")
				},
			},
			{
				name: "contract not found",
				entry: registry.Entry{
					ID:   registry.NewID("test", "binding-notfound"),
					Kind: apidi.Binding,
					Data: NewMockPayload(&apidi.BindingConfig{
						Contracts: []apidi.BoundContractConfig{
							{
								Contract: "test:missing",
								Methods:  map[string]string{"method1": "test:func1"},
							},
						},
					}),
				},
				expectedKind: apierror.Invalid,
				expectedMsg:  "binding references undefined contract",
				assertDetails: func(t *testing.T, apiErr apierror.Error) {
					assertDetailString(t, apiErr, "binding_id", "test:binding-notfound")
					assertDetailInt(t, apiErr, "contract_index", 0)
					assertDetailString(t, apiErr, "contract_id", "test:missing")
				},
			},
			{
				name: "missing method in binding",
				entry: registry.Entry{
					ID:   registry.NewID("test", "binding-missing-method"),
					Kind: apidi.Binding,
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
				expectedKind: apierror.Invalid,
				expectedMsg:  "contract method is not bound",
				assertDetails: func(t *testing.T, apiErr apierror.Error) {
					assertDetailString(t, apiErr, "binding_id", "test:binding-missing-method")
					assertDetailString(t, apiErr, "contract_id", defID.String())
					assertDetailString(t, apiErr, "method_name", "method2")
				},
			},
			{
				name: "extra method in binding",
				entry: registry.Entry{
					ID:   registry.NewID("test", "binding-extra-method"),
					Kind: apidi.Binding,
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
				expectedKind: apierror.Invalid,
				expectedMsg:  "bound method is not defined in contract definition",
				assertDetails: func(t *testing.T, apiErr apierror.Error) {
					assertDetailString(t, apiErr, "binding_id", "test:binding-extra-method")
					assertDetailString(t, apiErr, "contract_id", defID.String())
					assertDetailString(t, apiErr, "method_name", "method999")
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := manager.Add(ctx, tt.entry)
				apiErr := requireAPIError(t, err, tt.expectedKind, tt.expectedMsg)
				if tt.assertDetails != nil {
					tt.assertDetails(t, apiErr)
				}
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
	defID1 := registry.NewID("test", "contract1")
	defID2 := registry.NewID("test", "contract2")

	defEntry1 := registry.Entry{
		ID:   defID1,
		Kind: apidi.Definition,
		Data: NewMockPayload(&apidi.DefinitionConfig{
			Methods: []apidi.MethodConfig{{Name: "method1"}},
		}),
	}

	defEntry2 := registry.Entry{
		ID:   defID2,
		Kind: apidi.Definition,
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
			ID:   registry.NewID("test", "binding1"),
			Kind: apidi.Binding,
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
			ID:   registry.NewID("test", "binding2"),
			Kind: apidi.Binding,
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
		apiErr := requireAPIError(t, err, apierror.AlreadyExists, "contract already has default binding")
		assertDetailString(t, apiErr, "contract_id", defID1.String())
		assertDetailString(t, apiErr, "existing_binding_id", "test:binding1")
		assertDetailString(t, apiErr, "new_binding_id", "test:binding2")
	})

	t.Run("different contracts can have defaults", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.NewID("test", "binding3"),
			Kind: apidi.Binding,
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
			ID:   registry.NewID("test", "binding4"),
			Kind: apidi.Binding,
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
		bindingID := registry.NewID("test", "binding5")
		entry := registry.Entry{
			ID:   bindingID,
			Kind: apidi.Binding,
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
			Kind: apidi.Binding,
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
		apiErr := requireAPIError(t, err, apierror.AlreadyExists, "contract already has default binding")
		assertDetailString(t, apiErr, "contract_id", defID1.String())
		assertDetailString(t, apiErr, "existing_binding_id", "test:binding1")
		assertDetailString(t, apiErr, "new_binding_id", "test:binding5")
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
	defID := registry.NewID("test", "def1")
	defEntry := registry.Entry{
		ID:   defID,
		Kind: apidi.Definition,
		Data: NewMockPayload(&apidi.DefinitionConfig{
			Methods: []apidi.MethodConfig{{Name: "method1"}},
		}),
	}

	bindingEntry := registry.Entry{
		ID:   registry.NewID("test", "binding1"),
		Kind: apidi.Binding,
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
		apiErr := requireAPIError(t, err, apierror.Invalid, "contract definition is in use by binding")
		assertDetailString(t, apiErr, "definition_id", defID.String())
		assertDetailString(t, apiErr, "binding_id", bindingEntry.ID.String())
	})

	t.Run("definition update invalidates bindings", func(t *testing.T) {
		// Try to update definition to remove method1 - should fail
		updatedDefEntry := registry.Entry{
			ID:   defID,
			Kind: apidi.Definition,
			Data: NewMockPayload(&apidi.DefinitionConfig{
				Methods: []apidi.MethodConfig{{Name: "method2"}}, // Different method
			}),
		}

		err := manager.Update(ctx, updatedDefEntry)
		apiErr := requireAPIError(t, err, apierror.Invalid, "definition update would invalidate binding")
		assertDetailString(t, apiErr, "definition_id", defID.String())
		assertDetailString(t, apiErr, "binding_id", bindingEntry.ID.String())
		assert.NotEmpty(t, apiErr.Details().GetString("cause", ""))
	})
}

func TestManager_UnmarshalError(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, _ := setupDIManagerTest()

	// Configure transcoder to return error
	manager.dtt = &MockTranscoder{unmarshalError: fmt.Errorf("unmarshal failed")}

	entry := registry.Entry{
		ID:   registry.NewID("test", "unmarshal_error"),
		Kind: apidi.Definition,
		Data: NewMockPayload("invalid data"),
	}

	err := manager.Add(ctx, entry)
	apiErr := requireAPIError(t, err, apierror.Invalid, "failed to decode definition")
	assertDetailString(t, apiErr, "definition_id", entry.ID.String())
	assert.NotEmpty(t, apiErr.Details().GetString("cause", ""))
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
		registry.NewID("test", "contract1"),
		registry.NewID("test", "contract2"),
		registry.NewID("test", "contract3"),
	}

	for i, contractID := range contractIDs {
		defEntry := registry.Entry{
			ID:   contractID,
			Kind: apidi.Definition,
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
			ID:   registry.NewID("test", "multi_contract_binding"),
			Kind: apidi.Binding,
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
			ID:   registry.NewID("test", "conflicting_default"),
			Kind: apidi.Binding,
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
		apiErr := requireAPIError(t, err, apierror.AlreadyExists, "contract already has default binding")
		assertDetailString(t, apiErr, "contract_id", contractIDs[0].String())
		assertDetailString(t, apiErr, "existing_binding_id", "test:multi_contract_binding")
		assertDetailString(t, apiErr, "new_binding_id", "test:conflicting_default")
	})

	t.Run("attempt to add conflicting default for contract3", func(t *testing.T) {
		// Try to add another binding with default=true for contract3 (should fail)
		entry := registry.Entry{
			ID:   registry.NewID("test", "conflicting_default3"),
			Kind: apidi.Binding,
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
		apiErr := requireAPIError(t, err, apierror.AlreadyExists, "contract already has default binding")
		assertDetailString(t, apiErr, "contract_id", contractIDs[2].String())
		assertDetailString(t, apiErr, "existing_binding_id", "test:multi_contract_binding")
		assertDetailString(t, apiErr, "new_binding_id", "test:conflicting_default3")
	})

	t.Run("can add default for contract2 (no existing default)", func(t *testing.T) {
		// contract2 doesn't have a default yet, so this should work
		entry := registry.Entry{
			ID:   registry.NewID("test", "contract2_default"),
			Kind: apidi.Binding,
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
			ID:   registry.NewID("test", "non_default_binding"),
			Kind: apidi.Binding,
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
		registry.NewID("test", "service1"),
		registry.NewID("test", "service2"),
	}

	for i, contractID := range contractIDs {
		defEntry := registry.Entry{
			ID:   contractID,
			Kind: apidi.Definition,
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
	bindingAID := registry.NewID("test", "bindingA")
	bindingBID := registry.NewID("test", "bindingB")

	// BindingA: default for service1, non-default for service2
	entryA := registry.Entry{
		ID:   bindingAID,
		Kind: apidi.Binding,
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
		Kind: apidi.Binding,
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
			Kind: apidi.Binding,
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
		apiErr := requireAPIError(t, err, apierror.AlreadyExists, "contract already has default binding")
		assertDetailString(t, apiErr, "contract_id", contractIDs[1].String())
		assertDetailString(t, apiErr, "existing_binding_id", "test:bindingB")
		assertDetailString(t, apiErr, "new_binding_id", "test:bindingA")
	})

	t.Run("remove default from bindingA for service1", func(t *testing.T) {
		// Update bindingA to remove its default status for service1
		updateEntry := registry.Entry{
			ID:   bindingAID,
			Kind: apidi.Binding,
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
			Kind: apidi.Binding,
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
			Kind: apidi.Binding,
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
			Kind: apidi.Binding,
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
	contractID := registry.NewID("test", "delete_test_contract")
	defEntry := registry.Entry{
		ID:   contractID,
		Kind: apidi.Definition,
		Data: NewMockPayload(&apidi.DefinitionConfig{
			Methods: []apidi.MethodConfig{{Name: "execute"}},
		}),
	}

	wg.Add(1)
	err = manager.Add(ctx, defEntry)
	require.NoError(t, err)
	wg.Wait()

	// Add default binding
	defaultBindingID := registry.NewID("test", "default_impl")
	defaultEntry := registry.Entry{
		ID:   defaultBindingID,
		Kind: apidi.Binding,
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
	altBindingID := registry.NewID("test", "alt_impl")
	altEntry := registry.Entry{
		ID:   altBindingID,
		Kind: apidi.Binding,
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
		newDefaultID := registry.NewID("test", "new_default_impl")
		newDefaultEntry := registry.Entry{
			ID:   newDefaultID,
			Kind: apidi.Binding,
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
	contractID := registry.NewID("service", "payment_processor")
	defEntry := registry.Entry{
		ID:   contractID,
		Kind: apidi.Definition,
		Data: NewMockPayload(&apidi.DefinitionConfig{
			Methods: []apidi.MethodConfig{{Name: "process_payment"}},
		}),
	}

	wg.Add(1)
	err = manager.Add(ctx, defEntry)
	require.NoError(t, err)
	wg.Wait()

	// Add first default binding
	firstDefaultID := registry.NewID("impl", "stripe_payment")
	firstEntry := registry.Entry{
		ID:   firstDefaultID,
		Kind: apidi.Binding,
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
		secondDefaultID := registry.NewID("impl", "paypal_payment")
		secondEntry := registry.Entry{
			ID:   secondDefaultID,
			Kind: apidi.Binding,
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
		apiErr := requireAPIError(t, err, apierror.AlreadyExists, "contract already has default binding")
		assertDetailString(t, apiErr, "contract_id", contractID.String())
		assertDetailString(t, apiErr, "existing_binding_id", firstDefaultID.String())
		assertDetailString(t, apiErr, "new_binding_id", secondDefaultID.String())
	})

	t.Run("update error message is equally descriptive", func(t *testing.T) {
		// Add a non-default binding first
		nonDefaultID := registry.NewID("impl", "square_payment")
		nonDefaultEntry := registry.Entry{
			ID:   nonDefaultID,
			Kind: apidi.Binding,
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
			Kind: apidi.Binding,
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
		apiErr := requireAPIError(t, err, apierror.AlreadyExists, "contract already has default binding")
		assertDetailString(t, apiErr, "contract_id", contractID.String())
		assertDetailString(t, apiErr, "existing_binding_id", firstDefaultID.String())
		assertDetailString(t, apiErr, "new_binding_id", nonDefaultID.String())
	})
}
