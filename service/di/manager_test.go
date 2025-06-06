package di

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/ponyruntime/pony/api/contract"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	apidi "github.com/ponyruntime/pony/api/service/di"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func (m *MockTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
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
	ctx := context.Background()
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
			ID:   registry.ID{NS: "test", Name: "def1"},
			Kind: apidi.KindDefinition,
			Data: NewMockPayload(&apidi.DefinitionConfig{
				Description: "Test definition",
				Methods: []apidi.MethodConfig{
					{
						Name:        "testMethod",
						Description: "Test method",
						InputSchema: apidi.SchemaConfig{
							Format:     "application/json",
							Definition: json.RawMessage(`{"type": "object"}`),
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
		assert.Equal(t, entry.ID.String(), events[0].Path)
		mu.Unlock()
	})

	t.Run("wrong entry kind", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "invalid"},
			Kind: "invalid.kind",
			Data: NewMockPayload(&apidi.DefinitionConfig{}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
	})

	t.Run("nil data", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "nildata"},
			Kind: apidi.KindDefinition,
			Data: nil,
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "definition data is required")
	})

	t.Run("empty method name", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "empty_method"},
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
			ID:   registry.ID{NS: "test", Name: "dup_methods"},
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
						InputSchema: apidi.SchemaConfig{
							Definition: json.RawMessage(`{"type": "object"}`),
							// Missing Format
						},
					},
				},
			}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "input schema for method 'method1' in definition 'test:no_format' has a definition but no format specified")
	})

	t.Run("duplicate definition", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "def1"}, // Same as successful test
			Kind: apidi.KindDefinition,
			Data: NewMockPayload(&apidi.DefinitionConfig{}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})
}

func TestManager_DefinitionUpdate(t *testing.T) {
	ctx := context.Background()
	manager, bus := setupDIManagerTest()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterDefinition, func(evt event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer sub.Close()

	updateSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.UpdateDefinition, func(evt event.Event) {
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
				Description: "Updated definition",
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
			ID:   registry.ID{NS: "test", Name: "missing"},
			Kind: apidi.KindDefinition,
			Data: NewMockPayload(&apidi.DefinitionConfig{}),
		}

		err := manager.Update(ctx, updateEntry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found for update")
	})
}

func TestManager_DefinitionDelete(t *testing.T) {
	ctx := context.Background()
	manager, bus := setupDIManagerTest()

	var wg sync.WaitGroup
	addSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterDefinition, func(evt event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer addSub.Close()

	deleteSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.DeleteDefinition, func(evt event.Event) {
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
	ctx := context.Background()
	manager, bus := setupDIManagerTest()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterDefinition, func(evt event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer sub.Close()

	bindingSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterBinding, func(evt event.Event) {
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
			ID:   registry.ID{NS: "test", Name: "binding1"},
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
					ID:   registry.ID{NS: "test", Name: "invalid"},
					Kind: "invalid.kind",
					Data: NewMockPayload(&apidi.BindingConfig{}),
				},
				expectError: "unsupported entry kind",
			},
			{
				name: "nil data",
				entry: registry.Entry{
					ID:   registry.ID{NS: "test", Name: "nildata"},
					Kind: apidi.KindBinding,
					Data: nil,
				},
				expectError: "binding data is required",
			},
			{
				name: "empty contracts",
				entry: registry.Entry{
					ID:   registry.ID{NS: "test", Name: "empty"},
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
					ID:   registry.ID{NS: "test", Name: "missing_contract"},
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
					ID:   registry.ID{NS: "test", Name: "missing_method"},
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
					ID:   registry.ID{NS: "test", Name: "extra_method"},
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

func TestManager_ValidationEdgeCases(t *testing.T) {
	ctx := context.Background()
	manager, bus := setupDIManagerTest()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterDefinition, func(evt event.Event) {
		wg.Done()
	})
	require.NoError(t, err)
	defer sub.Close()

	bindingSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.RegisterBinding, func(evt event.Event) {
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
		ID:   registry.ID{NS: "test", Name: "binding1"},
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
	ctx := context.Background()
	manager, _ := setupDIManagerTest()

	// Configure transcoder to return error
	manager.dtt = &MockTranscoder{unmarshalError: fmt.Errorf("unmarshal failed")}

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "error"},
		Kind: apidi.KindDefinition,
		Data: NewMockPayload("invalid data"),
	}

	err := manager.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode definition")
}
