package contract

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func setupRegistryTest() (*Registry, event.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	contractRegistry := NewContractRegistry(bus, logger)
	return contractRegistry, bus
}

func TestNewContractRegistry(t *testing.T) {
	bus := eventbus.NewBus()
	logger := zap.NewNop()

	reg := NewContractRegistry(bus, logger)
	assert.NotNil(t, reg)
	assert.Equal(t, bus, reg.bus)
	assert.Equal(t, logger, reg.logger)
	assert.NotNil(t, reg.definitions)
	assert.NotNil(t, reg.bindings)
	assert.NotNil(t, reg.defaultBindings)
}

func TestContractRegistry_StartStop(t *testing.T) {
	ctx := context.Background()
	contractRegistry, _ := setupRegistryTest()

	err := contractRegistry.Start(ctx)
	require.NoError(t, err)
	assert.Equal(t, ctx, contractRegistry.ctx)
	assert.NotNil(t, contractRegistry.subscriber)

	err = contractRegistry.Stop()
	require.NoError(t, err)
}

func TestContractRegistry_DefinitionEvents(t *testing.T) {
	ctx := context.Background()
	contractRegistry, bus := setupRegistryTest()
	require.NoError(t, contractRegistry.Start(ctx))
	defer func() {
		require.NoError(t, contractRegistry.Stop())
	}()

	// Setup subscriber to listen for Accept/Reject events
	var responses []event.Event
	var mu sync.Mutex
	var wg sync.WaitGroup

	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		contract.System,
		"contract.*",
		func(evt event.Event) {
			if evt.Kind == contract.Accept || evt.Kind == contract.Reject {
				mu.Lock()
				responses = append(responses, evt)
				mu.Unlock()
				wg.Done()
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	testDef := &contract.Definition{
		Methods: []contract.MethodDef{
			{
				Name:        "testMethod",
				Description: "Test method",
				InputSchemas: []contract.SchemaDefinition{
					{
						Format:     "application/schema+json",
						Definition: map[string]interface{}{"type": "object"},
					},
				},
				OutputSchemas: []contract.SchemaDefinition{
					{
						Format:     "application/schema+json",
						Definition: map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}

	tests := []struct {
		name         string
		eventKind    event.Kind
		eventData    interface{}
		expectedKind event.Kind
	}{
		{
			name:         "register definition success",
			eventKind:    contract.RegisterDefinition,
			eventData:    testDef,
			expectedKind: contract.Accept,
		},
		{
			name:         "update definition success",
			eventKind:    contract.UpdateDefinition,
			eventData:    testDef,
			expectedKind: contract.Accept,
		},
		{
			name:         "delete definition success",
			eventKind:    contract.DeleteDefinition,
			eventData:    nil,
			expectedKind: contract.Accept,
		},
		{
			name:         "register definition with invalid payload",
			eventKind:    contract.RegisterDefinition,
			eventData:    "invalid",
			expectedKind: contract.Reject,
		},
		{
			name:         "update definition with invalid payload",
			eventKind:    contract.UpdateDefinition,
			eventData:    123,
			expectedKind: contract.Reject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responses = nil // Clear previous responses
			wg.Add(1)       // Expect one response event

			evt := event.Event{
				System: contract.System,
				Kind:   tt.eventKind,
				Path:   "test:contract",
				Data:   tt.eventData,
			}

			bus.Send(ctx, evt)

			// Wait for response with timeout
			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()

			select {
			case <-done:
				// Success - continue with checks
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for response event")
			}

			// Check the response
			mu.Lock()
			require.NotEmpty(t, responses, "no response received")
			lastResponse := responses[len(responses)-1]
			mu.Unlock()

			assert.Equal(t, contract.System, lastResponse.System)
			assert.Equal(t, tt.expectedKind, lastResponse.Kind)
			assert.Equal(t, "test:contract", lastResponse.Path)
		})
	}
}

func TestContractRegistry_BindingEvents(t *testing.T) {
	ctx := context.Background()
	contractRegistry, bus := setupRegistryTest()
	require.NoError(t, contractRegistry.Start(ctx))
	defer func() {
		require.NoError(t, contractRegistry.Stop())
	}()

	// Setup subscriber to listen for Accept/Reject events
	var responses []event.Event
	var mu sync.Mutex
	var wg sync.WaitGroup

	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		contract.System,
		"contract.*",
		func(evt event.Event) {
			if evt.Kind == contract.Accept || evt.Kind == contract.Reject {
				mu.Lock()
				responses = append(responses, evt)
				mu.Unlock()
				wg.Done()
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	testBinding := &contract.Binding{
		Contracts: []contract.BoundContract{
			{
				Contract:        registry.NewID("test", "contract"),
				Methods:         map[string]registry.ID{"method1": registry.NewID("test", "func1")},
				ContextRequired: []string{"scope1"},
			},
		},
	}

	tests := []struct {
		name         string
		eventKind    event.Kind
		eventData    interface{}
		expectedKind event.Kind
	}{
		{
			name:         "register binding success",
			eventKind:    contract.RegisterBinding,
			eventData:    testBinding,
			expectedKind: contract.Accept,
		},
		{
			name:         "update binding success",
			eventKind:    contract.UpdateBinding,
			eventData:    testBinding,
			expectedKind: contract.Accept,
		},
		{
			name:         "delete binding success",
			eventKind:    contract.DeleteBinding,
			eventData:    nil,
			expectedKind: contract.Accept,
		},
		{
			name:         "register binding with invalid payload",
			eventKind:    contract.RegisterBinding,
			eventData:    "invalid",
			expectedKind: contract.Reject,
		},
		{
			name:         "update binding with invalid payload",
			eventKind:    contract.UpdateBinding,
			eventData:    []string{"invalid"},
			expectedKind: contract.Reject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responses = nil // Clear previous responses
			wg.Add(1)       // Expect one response event

			evt := event.Event{
				System: contract.System,
				Kind:   tt.eventKind,
				Path:   "test:binding",
				Data:   tt.eventData,
			}

			bus.Send(ctx, evt)

			// Wait for response with timeout
			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()

			select {
			case <-done:
				// Success - continue with checks
			case <-time.After(time.Second):
				t.Fatal("timeout waiting for response event")
			}

			// Check the response
			mu.Lock()
			require.NotEmpty(t, responses, "no response received")
			lastResponse := responses[len(responses)-1]
			mu.Unlock()

			assert.Equal(t, contract.System, lastResponse.System)
			assert.Equal(t, tt.expectedKind, lastResponse.Kind)
			assert.Equal(t, "test:binding", lastResponse.Path)
		})
	}
}

func TestContractRegistry_UnknownEvent(t *testing.T) {
	ctx := context.Background()
	contractRegistry, bus := setupRegistryTest()
	require.NoError(t, contractRegistry.Start(ctx))
	defer func() {
		require.NoError(t, contractRegistry.Stop())
	}()

	evt := event.Event{
		System: contract.System,
		Kind:   "unknown.event",
		Path:   "test:something",
		Data:   nil,
	}

	// Should not panic
	bus.Send(ctx, evt)
	time.Sleep(10 * time.Millisecond) // Allow processing
}

func TestContractRegistry_GetContract(t *testing.T) {
	ctx := context.Background()
	contractRegistry, bus := setupRegistryTest()
	require.NoError(t, contractRegistry.Start(ctx))
	defer func() {
		require.NoError(t, contractRegistry.Stop())
	}()

	// Setup subscriber to wait for registration
	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		contract.System,
		"contract.*",
		func(evt event.Event) {
			if evt.Kind == contract.Accept {
				wg.Done()
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	// Register a test definition
	testDef := &contract.Definition{
		Methods: []contract.MethodDef{
			{
				Name:        "testMethod",
				Description: "Test method",
			},
		},
	}

	contractID := registry.NewID("test", "contract")
	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterDefinition,
		Path:   contractID.String(),
		Data:   testDef,
	})
	wg.Wait() // Wait for registration to complete

	// Test successful retrieval
	contractObj, err := contractRegistry.GetContract(ctx, contractID)
	require.NoError(t, err)
	assert.NotNil(t, contractObj)
	assert.Len(t, contractObj.Methods(), 1)
	assert.Equal(t, "testMethod", contractObj.Methods()[0].Name)

	// Test method retrieval
	method, err := contractObj.Method("testMethod")
	require.NoError(t, err)
	assert.Equal(t, "testMethod", method.Name)

	// Test non-existent method
	_, err = contractObj.Method("nonExistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "method 'nonExistent' not found")

	// Test non-existent contract
	_, err = contractRegistry.GetContract(ctx, registry.NewID("test", "missing"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contract definition 'test:missing' not found")
}

func TestContractRegistry_GetBinding(t *testing.T) {
	ctx := context.Background()
	contractRegistry, bus := setupRegistryTest()
	require.NoError(t, contractRegistry.Start(ctx))
	defer func() {
		require.NoError(t, contractRegistry.Stop())
	}()

	// Setup subscriber to wait for registration
	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		contract.System,
		"contract.*",
		func(evt event.Event) {
			if evt.Kind == contract.Accept {
				wg.Done()
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	// Register a test binding
	bindingID := registry.NewID("test", "binding")
	testBinding := &contract.Binding{
		Meta: attrs.Bag{"key": "value"},
		Contracts: []contract.BoundContract{
			{
				Contract: registry.NewID("test", "contract"),
				Methods:  map[string]registry.ID{"method1": registry.NewID("test", "func1")},
			},
		},
	}

	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterBinding,
		Path:   bindingID.String(),
		Data:   testBinding,
	})
	wg.Wait() // Wait for registration to complete

	// Test successful retrieval
	binding, err := contractRegistry.GetBinding(ctx, bindingID)
	require.NoError(t, err)
	assert.NotNil(t, binding)
	assert.Equal(t, "value", binding.Meta["key"])
	assert.Len(t, binding.Contracts, 1)

	// Test non-existent binding
	_, err = contractRegistry.GetBinding(ctx, registry.NewID("test", "missing"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contract binding 'test:missing' not found")
}

func TestContractRegistry_GetBindingsForContract(t *testing.T) {
	ctx := context.Background()
	contractRegistry, bus := setupRegistryTest()
	require.NoError(t, contractRegistry.Start(ctx))
	defer func() {
		require.NoError(t, contractRegistry.Stop())
	}()

	// Setup subscriber to wait for registrations
	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		contract.System,
		"contract.*",
		func(evt event.Event) {
			if evt.Kind == contract.Accept {
				wg.Done()
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	contractID := registry.NewID("test", "contract")
	binding1ID := registry.NewID("test", "binding1")
	binding2ID := registry.NewID("test", "binding2")
	binding3ID := registry.NewID("test", "binding3")

	// Register bindings
	testBinding1 := &contract.Binding{
		Contracts: []contract.BoundContract{
			{
				Contract: contractID,
				Methods:  map[string]registry.ID{"method1": registry.NewID("test", "func1")},
			},
		},
	}

	testBinding2 := &contract.Binding{
		Contracts: []contract.BoundContract{
			{
				Contract: contractID,
				Methods:  map[string]registry.ID{"method2": registry.NewID("test", "func2")},
			},
		},
	}

	testBinding3 := &contract.Binding{
		Contracts: []contract.BoundContract{
			{
				Contract: registry.NewID("other", "contract"), // Different contract
				Methods:  map[string]registry.ID{"method3": registry.NewID("test", "func3")},
			},
		},
	}

	wg.Add(3) // Expect 3 registrations
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterBinding,
		Path:   binding1ID.String(),
		Data:   testBinding1,
	})
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterBinding,
		Path:   binding2ID.String(),
		Data:   testBinding2,
	})
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterBinding,
		Path:   binding3ID.String(),
		Data:   testBinding3,
	})
	wg.Wait() // Wait for all registrations to complete

	// Test getting bindings for contract
	bindingIDs, err := contractRegistry.GetBindingsForContract(ctx, contractID)
	require.NoError(t, err)
	assert.Len(t, bindingIDs, 2)

	// Should contain binding1 and binding2, but not binding3
	foundBinding1 := false
	foundBinding2 := false
	for _, id := range bindingIDs {
		if id == binding1ID {
			foundBinding1 = true
		}
		if id == binding2ID {
			foundBinding2 = true
		}
		assert.NotEqual(t, binding3ID, id) // Should not contain binding3
	}
	assert.True(t, foundBinding1)
	assert.True(t, foundBinding2)

	// Test with non-existent contract
	bindingIDs, err = contractRegistry.GetBindingsForContract(ctx, registry.NewID("missing", "contract"))
	require.NoError(t, err)
	assert.Empty(t, bindingIDs)
}

func TestContractRegistry_GetDefaultBinding(t *testing.T) {
	ctx := context.Background()
	contractRegistry, bus := setupRegistryTest()
	require.NoError(t, contractRegistry.Start(ctx))
	defer func() {
		require.NoError(t, contractRegistry.Stop())
	}()

	// Setup subscriber to wait for registrations
	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		contract.System,
		"contract.*",
		func(evt event.Event) {
			if evt.Kind == contract.Accept {
				wg.Done()
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	contractID := registry.NewID("test", "contract")
	defaultBindingID := registry.NewID("test", "default_binding")
	nonDefaultBindingID := registry.NewID("test", "non_default_binding")

	t.Run("no default binding exists", func(t *testing.T) {
		// Test getting default for non-existent contract
		_, err := contractRegistry.GetDefaultBinding(ctx, contractID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no default binding for contract")
	})

	t.Run("register default binding", func(t *testing.T) {
		defaultBinding := &contract.Binding{
			Contracts: []contract.BoundContract{
				{
					Contract: contractID,
					Methods:  map[string]registry.ID{"method1": registry.NewID("test", "func1")},
					Default:  true, // Mark as default
				},
			},
		}

		wg.Add(1)
		bus.Send(ctx, event.Event{
			System: contract.System,
			Kind:   contract.RegisterBinding,
			Path:   defaultBindingID.String(),
			Data:   defaultBinding,
		})
		wg.Wait()

		// Should now return the default binding
		result, err := contractRegistry.GetDefaultBinding(ctx, contractID)
		require.NoError(t, err)
		assert.Equal(t, defaultBindingID, result)
	})

	t.Run("register non-default binding", func(t *testing.T) {
		nonDefaultBinding := &contract.Binding{
			Contracts: []contract.BoundContract{
				{
					Contract: contractID,
					Methods:  map[string]registry.ID{"method2": registry.NewID("test", "func2")},
					Default:  false, // Explicitly non-default
				},
			},
		}

		wg.Add(1)
		bus.Send(ctx, event.Event{
			System: contract.System,
			Kind:   contract.RegisterBinding,
			Path:   nonDefaultBindingID.String(),
			Data:   nonDefaultBinding,
		})
		wg.Wait()

		// Should still return the original default binding
		result, err := contractRegistry.GetDefaultBinding(ctx, contractID)
		require.NoError(t, err)
		assert.Equal(t, defaultBindingID, result)
	})

	t.Run("update binding to remove default", func(t *testing.T) {
		nonDefaultBinding := &contract.Binding{
			Contracts: []contract.BoundContract{
				{
					Contract: contractID,
					Methods:  map[string]registry.ID{"method1": registry.NewID("test", "func1")},
					Default:  false, // Remove default
				},
			},
		}

		wg.Add(1)
		bus.Send(ctx, event.Event{
			System: contract.System,
			Kind:   contract.UpdateBinding,
			Path:   defaultBindingID.String(),
			Data:   nonDefaultBinding,
		})
		wg.Wait()

		// Should now have no default binding
		_, err := contractRegistry.GetDefaultBinding(ctx, contractID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no default binding for contract")
	})

	t.Run("update non-default binding to be default", func(t *testing.T) {
		newDefaultBinding := &contract.Binding{
			Contracts: []contract.BoundContract{
				{
					Contract: contractID,
					Methods:  map[string]registry.ID{"method2": registry.NewID("test", "func2")},
					Default:  true, // Make this the default
				},
			},
		}

		wg.Add(1)
		bus.Send(ctx, event.Event{
			System: contract.System,
			Kind:   contract.UpdateBinding,
			Path:   nonDefaultBindingID.String(),
			Data:   newDefaultBinding,
		})
		wg.Wait()

		// Should now return the updated default binding
		result, err := contractRegistry.GetDefaultBinding(ctx, contractID)
		require.NoError(t, err)
		assert.Equal(t, nonDefaultBindingID, result)
	})

	t.Run("delete default binding", func(t *testing.T) {
		wg.Add(1)
		bus.Send(ctx, event.Event{
			System: contract.System,
			Kind:   contract.DeleteBinding,
			Path:   nonDefaultBindingID.String(),
		})
		wg.Wait()

		// Should now have no default binding
		_, err := contractRegistry.GetDefaultBinding(ctx, contractID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no default binding for contract")
	})
}

func TestContractRegistry_DefaultBindingCleanupOnContractDelete(t *testing.T) {
	ctx := context.Background()
	contractRegistry, bus := setupRegistryTest()
	require.NoError(t, contractRegistry.Start(ctx))
	defer func() {
		require.NoError(t, contractRegistry.Stop())
	}()

	// Setup subscriber to wait for registrations
	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		contract.System,
		"contract.*",
		func(evt event.Event) {
			if evt.Kind == contract.Accept {
				wg.Done()
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	contractID := registry.NewID("test", "contract")
	bindingID := registry.NewID("test", "binding")

	// Register a definition
	testDef := &contract.Definition{
		Methods: []contract.MethodDef{{Name: "method1"}},
	}

	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterDefinition,
		Path:   contractID.String(),
		Data:   testDef,
	})
	wg.Wait()

	// Register a default binding
	defaultBinding := &contract.Binding{
		Contracts: []contract.BoundContract{
			{
				Contract: contractID,
				Methods:  map[string]registry.ID{"method1": registry.NewID("test", "func1")},
				Default:  true,
			},
		},
	}

	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterBinding,
		Path:   bindingID.String(),
		Data:   defaultBinding,
	})
	wg.Wait()

	// Verify default binding exists
	result, err := contractRegistry.GetDefaultBinding(ctx, contractID)
	require.NoError(t, err)
	assert.Equal(t, bindingID, result)

	// Delete the contract definition
	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.DeleteDefinition,
		Path:   contractID.String(),
	})
	wg.Wait()

	// Verify default binding is cleaned up
	_, err = contractRegistry.GetDefaultBinding(ctx, contractID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no default binding for contract")
}

func TestContractRegistry_MultipleContractsInBinding(t *testing.T) {
	ctx := context.Background()
	contractRegistry, bus := setupRegistryTest()
	require.NoError(t, contractRegistry.Start(ctx))
	defer func() {
		require.NoError(t, contractRegistry.Stop())
	}()

	// Setup subscriber to wait for registrations
	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		contract.System,
		"contract.*",
		func(evt event.Event) {
			if evt.Kind == contract.Accept {
				wg.Done()
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	contract1ID := registry.NewID("test", "contract1")
	contract2ID := registry.NewID("test", "contract2")
	bindingID := registry.NewID("test", "multi_binding")

	// Register a binding that implements multiple contracts with one as default
	multiBinding := &contract.Binding{
		Contracts: []contract.BoundContract{
			{
				Contract: contract1ID,
				Methods:  map[string]registry.ID{"method1": registry.NewID("test", "func1")},
				Default:  true, // This contract is default
			},
			{
				Contract: contract2ID,
				Methods:  map[string]registry.ID{"method2": registry.NewID("test", "func2")},
				Default:  false, // This contract is not default
			},
		},
	}

	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterBinding,
		Path:   bindingID.String(),
		Data:   multiBinding,
	})
	wg.Wait()

	// Test that contract1 has a default binding
	result1, err := contractRegistry.GetDefaultBinding(ctx, contract1ID)
	require.NoError(t, err)
	assert.Equal(t, bindingID, result1)

	// Test that contract2 has no default binding
	_, err = contractRegistry.GetDefaultBinding(ctx, contract2ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no default binding for contract")

	// Update binding to make contract2 default and contract1 non-default
	updatedBinding := &contract.Binding{
		Contracts: []contract.BoundContract{
			{
				Contract: contract1ID,
				Methods:  map[string]registry.ID{"method1": registry.NewID("test", "func1")},
				Default:  false, // No longer default
			},
			{
				Contract: contract2ID,
				Methods:  map[string]registry.ID{"method2": registry.NewID("test", "func2")},
				Default:  true, // Now default
			},
		},
	}

	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.UpdateBinding,
		Path:   bindingID.String(),
		Data:   updatedBinding,
	})
	wg.Wait()

	// Test that contract1 no longer has a default binding
	_, err = contractRegistry.GetDefaultBinding(ctx, contract1ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no default binding for contract")

	// Test that contract2 now has a default binding
	result2, err := contractRegistry.GetDefaultBinding(ctx, contract2ID)
	require.NoError(t, err)
	assert.Equal(t, bindingID, result2)
}

func TestContractRegistry_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	contractRegistry, bus := setupRegistryTest()
	require.NoError(t, contractRegistry.Start(ctx))
	defer func() {
		require.NoError(t, contractRegistry.Stop())
	}()

	const numContracts = 10
	var wg sync.WaitGroup

	// Setup subscriber to wait for registrations
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		contract.System,
		"contract.*",
		func(evt event.Event) {
			if evt.Kind == contract.Accept {
				wg.Done()
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	// Register contracts concurrently
	for i := 0; i < numContracts; i++ {
		wg.Add(1) // Add before launching goroutine
		go func(idx int) {
			def := &contract.Definition{
				Methods: []contract.MethodDef{
					{
						Name:        fmt.Sprintf("method%d", idx),
						Description: fmt.Sprintf("Method %d", idx),
					},
				},
			}

			bus.Send(ctx, event.Event{
				System: contract.System,
				Kind:   contract.RegisterDefinition,
				Path:   fmt.Sprintf("test:contract-%d", idx),
				Data:   def,
			})
		}(i)
	}

	// Wait for all registrations to complete
	wg.Wait()

	// Verify all contracts were registered
	for i := 0; i < numContracts; i++ {
		contractID := registry.ID{NS: "test", Name: fmt.Sprintf("contract-%d", i)}
		contractObj, err := contractRegistry.GetContract(ctx, contractID)
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("method%d", i), contractObj.Methods()[0].Name)
	}
}

func TestContractImpl_ID_Meta(t *testing.T) {
	ctx := context.Background()
	contractRegistry, bus := setupRegistryTest()

	err := contractRegistry.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = contractRegistry.Stop() }()

	contractID := registry.NewID("test", "meta-contract")

	done := make(chan struct{})
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, "contract.*", func(e event.Event) {
		if e.Kind == contract.Accept && e.Path == contractID.String() {
			close(done)
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterDefinition,
		Path:   contractID.String(),
		Data: &contract.Definition{
			Meta: attrs.Bag{"key": "value"},
			Methods: []contract.MethodDef{
				{Name: "method1"},
			},
		},
	})

	<-done

	contractObj, err := contractRegistry.GetContract(ctx, contractID)
	require.NoError(t, err)

	assert.Equal(t, contractID, contractObj.ID())
	assert.Equal(t, "value", contractObj.Meta()["key"])
}

func TestContractRegistry_UnknownEventKind(t *testing.T) {
	ctx := context.Background()
	contractRegistry, bus := setupRegistryTest()

	err := contractRegistry.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = contractRegistry.Stop() }()

	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   "unknown.kind",
		Path:   "test:contract",
		Data:   nil,
	})

	time.Sleep(10 * time.Millisecond)
}

func TestContractRegistry_NilMetaInit(t *testing.T) {
	ctx := context.Background()
	contractRegistry, bus := setupRegistryTest()

	err := contractRegistry.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = contractRegistry.Stop() }()

	contractID := registry.NewID("test", "nil-meta")

	done := make(chan struct{})
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, "contract.*", func(e event.Event) {
		if e.Kind == contract.Accept && e.Path == contractID.String() {
			close(done)
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterDefinition,
		Path:   contractID.String(),
		Data: &contract.Definition{
			Meta:    nil,
			Methods: []contract.MethodDef{{Name: "method1"}},
		},
	})

	<-done

	contractObj, err := contractRegistry.GetContract(ctx, contractID)
	require.NoError(t, err)
	assert.NotNil(t, contractObj.Meta())
}
