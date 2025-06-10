package contract

import (
	"context"
	"fmt"
	"sync"
	"testing"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/contract"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/payload"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	functionSys "github.com/ponyruntime/pony/system/function"
	"github.com/ponyruntime/pony/system/pubsub"

	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupInstantiatorTest() (*Instantiator, event.Bus, *ContractRegistry, *functionSys.Registry) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	contractRegistry := NewContractRegistry(bus, logger)
	host := pubsub.NewHost(context.Background(), pubsub.HostConfig{BufferSize: 100})
	functionRegistry := functionSys.NewFunctionRegistry(bus, host, logger)

	instantiator := NewContractInstantiator(contractRegistry, functionRegistry)

	return instantiator, bus, contractRegistry, functionRegistry
}

func TestNewContractInstantiator(t *testing.T) {
	instantiator, _, contractRegistry, functionRegistry := setupInstantiatorTest()

	assert.NotNil(t, instantiator)
	assert.Equal(t, contractRegistry, instantiator.registry)
	assert.Equal(t, functionRegistry, instantiator.funcReg)
}

func TestInstantiator_Instantiate(t *testing.T) {
	ctx := context.Background()
	instantiator, bus, contractRegistry, _ := setupInstantiatorTest()

	require.NoError(t, contractRegistry.Start(ctx))
	defer func() {
		err := contractRegistry.Stop()
		require.NoError(t, err)
	}()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, "contract.*", func(evt event.Event) {
		if evt.Kind == contract.Accept {
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	// Register contract definition
	contractID := registry.ID{NS: "test", Name: "my_contract"}
	testDef := &contract.Definition{
		Methods: []contract.MethodDef{{Name: "testMethod"}},
	}

	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterDefinition,
		Path:   contractID.String(),
		Data:   testDef,
	})
	wg.Wait()

	// Register binding
	bindingID := registry.ID{NS: "test", Name: "my_binding"}
	testBinding := &contract.Binding{
		Meta: registry.Metadata{"version": "1.0"},
		Contracts: []contract.BoundContract{
			{
				Contract:        contractID,
				Methods:         map[string]registry.ID{"testMethod": {NS: "test", Name: "test_func"}},
				ContextRequired: []string{"required_key"},
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
	wg.Wait()

	// Test successful instantiation
	scope := registry.Metadata{"required_key": "value"}
	instance, err := instantiator.Instantiate(ctx, bindingID, scope)
	require.NoError(t, err)
	assert.Equal(t, bindingID, instance.ID())
	assert.Len(t, instance.Implements(), 1)

	// Test with non-existent binding
	_, err = instantiator.Instantiate(ctx, registry.ID{NS: "test", Name: "missing"}, registry.Metadata{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contract binding 'test:missing' not found")

	// Test with nil scope - should succeed
	instanceNil, err := instantiator.Instantiate(ctx, bindingID, nil)
	require.NoError(t, err)
	assert.NotNil(t, instanceNil)

	// Test with empty scope - should succeed
	instanceEmpty, err := instantiator.Instantiate(ctx, bindingID, registry.Metadata{})
	require.NoError(t, err)
	assert.NotNil(t, instanceEmpty)
}

func TestInstanceImpl_ScopeValidation(t *testing.T) {
	ctx := context.Background()
	instantiator, bus, contractRegistry, _ := setupInstantiatorTest()

	require.NoError(t, contractRegistry.Start(ctx))
	defer func() {
		err := contractRegistry.Stop()
		require.NoError(t, err)
	}()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, "contract.*", func(evt event.Event) {
		if evt.Kind == contract.Accept {
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	// Register contract
	contractID := registry.ID{NS: "test", Name: "scope_contract"}
	testDef := &contract.Definition{
		Methods: []contract.MethodDef{{Name: "validateMethod"}},
	}

	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterDefinition,
		Path:   contractID.String(),
		Data:   testDef,
	})
	wg.Wait()

	tests := []struct {
		name          string
		scopeRequired []string
		instanceScope registry.Metadata
		expectedError string
	}{
		{
			name:          "no scope required",
			scopeRequired: []string{},
			instanceScope: registry.Metadata{},
		},
		{
			name:          "required scope present",
			scopeRequired: []string{"key1", "key2"},
			instanceScope: registry.Metadata{"key1": "value1", "key2": "value2"},
		},
		{
			name:          "missing one required key",
			scopeRequired: []string{"key1", "key2"},
			instanceScope: registry.Metadata{"key1": "value1"},
			expectedError: "missing required scope keys: [key2]",
		},
		{
			name:          "missing all required keys",
			scopeRequired: []string{"key1", "key2"},
			instanceScope: registry.Metadata{},
			expectedError: "missing required scope keys: [key1 key2]",
		},
		{
			name:          "nil scope with required keys",
			scopeRequired: []string{"key1"},
			instanceScope: nil,
			expectedError: "missing required scope keys: [key1]",
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bindingID := registry.ID{NS: "test", Name: fmt.Sprintf("binding_%d", i)}
			testBinding := &contract.Binding{
				Contracts: []contract.BoundContract{
					{
						Contract:        contractID,
						Methods:         map[string]registry.ID{"validateMethod": {NS: "test", Name: "dummy_func"}},
						ContextRequired: tt.scopeRequired,
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
			wg.Wait()

			instance, err := instantiator.Instantiate(ctx, bindingID, tt.instanceScope)
			require.NoError(t, err)

			_, err = instance.Call(ctx, "validateMethod", payload.Payloads{})

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				// Success case - expect function not found since we didn't register dummy_func
				require.Error(t, err)
				assert.Contains(t, err.Error(), "no handler registered for target: test:dummy_func")
			}
		})
	}
}

func TestInstanceImpl_Call_Integration(t *testing.T) {
	ctx := context.Background()
	ctx = pubsubapi.WithNode(ctx, pubsub.NewNode("test", nil))

	instantiator, bus, contractRegistry, functionRegistry := setupInstantiatorTest()

	require.NoError(t, contractRegistry.Start(ctx))
	require.NoError(t, functionRegistry.Start(ctx))
	defer func() {
		err := contractRegistry.Stop()
		require.NoError(t, err)
		err = functionRegistry.Stop()
		require.NoError(t, err)
	}()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, "contract.*", func(evt event.Event) {
		if evt.Kind == contract.Accept {
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	// Register function
	funcID := registry.ID{NS: "test", Name: "test_func"}
	testFunc := function.Func(func(_ context.Context, task runtime.Task) (chan *runtime.Result, error) {
		resultChan := make(chan *runtime.Result, 1)
		resultChan <- &runtime.Result{Value: payload.New("function_result")}
		close(resultChan)
		return resultChan, nil
	})

	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data:   testFunc,
	})

	// Register contract
	contractID := registry.ID{NS: "test", Name: "my_contract"}
	testDef := &contract.Definition{
		Methods: []contract.MethodDef{{Name: "testMethod"}},
	}

	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterDefinition,
		Path:   contractID.String(),
		Data:   testDef,
	})
	wg.Wait()

	// Register binding
	bindingID := registry.ID{NS: "test", Name: "my_binding"}
	testBinding := &contract.Binding{
		Contracts: []contract.BoundContract{
			{
				Contract: contractID,
				Methods:  map[string]registry.ID{"testMethod": funcID},
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
	wg.Wait()

	// Create instance and call method
	instance, err := instantiator.Instantiate(ctx, bindingID, registry.Metadata{})
	require.NoError(t, err)

	resultChan, err := instance.Call(ctx, "testMethod", payload.Payloads{payload.New("test_input")})
	require.NoError(t, err)

	result := <-resultChan
	assert.Equal(t, "function_result", result.Value.Data().(string))

	// Test method not bound
	_, err = instance.Call(ctx, "unknownMethod", payload.Payloads{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "method 'unknownMethod' not bound")
}

func TestInstanceImpl_ContextMerging(t *testing.T) {
	ctx := context.Background()
	ctx = pubsubapi.WithNode(ctx, pubsub.NewNode("test", nil))

	instantiator, bus, contractRegistry, functionRegistry := setupInstantiatorTest()

	require.NoError(t, contractRegistry.Start(ctx))
	require.NoError(t, functionRegistry.Start(ctx))
	defer func() {
		err := contractRegistry.Stop()
		require.NoError(t, err)
		err = functionRegistry.Stop()
		require.NoError(t, err)
	}()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, "contract.*", func(evt event.Event) {
		if evt.Kind == contract.Accept {
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	// Function that checks context values
	funcID := registry.ID{NS: "test", Name: "context_func"}
	testFunc := function.Func(func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		resultChan := make(chan *runtime.Result, 1)

		result := map[string]interface{}{"has_context": false}
		if values, ok := ctx.Value(ctxapi.ValuesCtx).(*ctxapi.Contexter[any]); ok {
			existing, existingOk := values.Value("existing")
			scope, scopeOk := values.Value("scope")
			override, overrideOk := values.Value("override")

			result = map[string]interface{}{
				"has_context":    true,
				"existing_ok":    existingOk,
				"existing_value": existing,
				"scope_ok":       scopeOk,
				"scope_value":    scope,
				"override_ok":    overrideOk,
				"override_value": override,
			}
		}

		resultChan <- &runtime.Result{Value: payload.New(result)}
		close(resultChan)
		return resultChan, nil
	})

	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data:   testFunc,
	})

	// Register contract and binding
	contractID := registry.ID{NS: "test", Name: "context_contract"}
	testDef := &contract.Definition{
		Methods: []contract.MethodDef{{Name: "contextMethod"}},
	}

	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterDefinition,
		Path:   contractID.String(),
		Data:   testDef,
	})
	wg.Wait()

	bindingID := registry.ID{NS: "test", Name: "context_binding"}
	testBinding := &contract.Binding{
		Contracts: []contract.BoundContract{
			{
				Contract: contractID,
				Methods:  map[string]registry.ID{"contextMethod": funcID},
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
	wg.Wait()

	// Test with nil scope
	instanceNil, err := instantiator.Instantiate(ctx, bindingID, nil)
	require.NoError(t, err)

	resultChan, err := instanceNil.Call(ctx, "contextMethod", payload.Payloads{})
	require.NoError(t, err)

	result := <-resultChan
	values := result.Value.Data().(map[string]interface{})
	assert.False(t, values["has_context"].(bool))

	// Test context merging - scope values should be merged with existing context
	scope := registry.Metadata{
		"scope":    "from_scope",
		"override": "from_scope",
	}
	instance, err := instantiator.Instantiate(ctx, bindingID, scope)
	require.NoError(t, err)

	existing := ctxapi.NewContexter[any]()
	existing.SetValue("existing", "from_existing")
	existing.SetValue("override", "from_existing")
	callCtx := context.WithValue(ctx, ctxapi.ValuesCtx, existing)

	resultChan, err = instance.Call(callCtx, "contextMethod", payload.Payloads{})
	require.NoError(t, err)

	result = <-resultChan
	values = result.Value.Data().(map[string]interface{})

	assert.True(t, values["has_context"].(bool))
	assert.True(t, values["existing_ok"].(bool))
	assert.Equal(t, "from_existing", values["existing_value"])
	assert.True(t, values["scope_ok"].(bool))
	assert.Equal(t, "from_scope", values["scope_value"])
	assert.True(t, values["override_ok"].(bool))
	assert.Equal(t, "from_scope", values["override_value"]) // Scope wins over existing context
}

func TestInstanceImpl_ScopeContextBehavior(t *testing.T) {
	ctx := context.Background()
	ctx = pubsubapi.WithNode(ctx, pubsub.NewNode("test", nil))

	instantiator, bus, contractRegistry, functionRegistry := setupInstantiatorTest()

	require.NoError(t, contractRegistry.Start(ctx))
	require.NoError(t, functionRegistry.Start(ctx))
	defer func() {
		err := contractRegistry.Stop()
		require.NoError(t, err)
		err = functionRegistry.Stop()
		require.NoError(t, err)
	}()

	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, "contract.*", func(evt event.Event) {
		if evt.Kind == contract.Accept {
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	// Function that captures and returns all context values it receives
	funcID := registry.ID{NS: "test", Name: "capture_context_func"}
	testFunc := function.Func(func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		resultChan := make(chan *runtime.Result, 1)

		captured := map[string]interface{}{}
		if values, ok := ctx.Value(ctxapi.ValuesCtx).(*ctxapi.Contexter[any]); ok {
			// Capture all values from the context
			values.Iterate(func(key string, value any) {
				captured[key] = value
			})
		}

		resultChan <- &runtime.Result{Value: payload.New(captured)}
		close(resultChan)
		return resultChan, nil
	})

	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data:   testFunc,
	})

	// Register contract and binding
	contractID := registry.ID{NS: "test", Name: "capture_contract"}
	testDef := &contract.Definition{
		Methods: []contract.MethodDef{{Name: "captureMethod"}},
	}

	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterDefinition,
		Path:   contractID.String(),
		Data:   testDef,
	})
	wg.Wait()

	bindingID := registry.ID{NS: "test", Name: "capture_binding"}
	testBinding := &contract.Binding{
		Contracts: []contract.BoundContract{
			{
				Contract: contractID,
				Methods:  map[string]registry.ID{"captureMethod": funcID},
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
	wg.Wait()

	t.Run("empty scope produces no context", func(t *testing.T) {
		instance, err := instantiator.Instantiate(ctx, bindingID, registry.Metadata{})
		require.NoError(t, err)

		resultChan, err := instance.Call(ctx, "captureMethod", payload.Payloads{})
		require.NoError(t, err)

		result := <-resultChan
		captured := result.Value.Data().(map[string]interface{})

		// Should be empty since no scope was provided and no existing context
		assert.Empty(t, captured)
	})

	t.Run("scope values are properly passed to function", func(t *testing.T) {
		scope := registry.Metadata{
			"app_name":    "test_app",
			"version":     "1.0.0",
			"environment": "test",
			"feature_flags": map[string]bool{
				"new_feature": true,
				"old_feature": false,
			},
		}

		instance, err := instantiator.Instantiate(ctx, bindingID, scope)
		require.NoError(t, err)

		resultChan, err := instance.Call(ctx, "captureMethod", payload.Payloads{})
		require.NoError(t, err)

		result := <-resultChan
		captured := result.Value.Data().(map[string]interface{})

		// All scope values should be present in the context
		assert.Equal(t, "test_app", captured["app_name"])
		assert.Equal(t, "1.0.0", captured["version"])
		assert.Equal(t, "test", captured["environment"])
		assert.Contains(t, captured, "feature_flags")
	})

	t.Run("scope merges with existing context", func(t *testing.T) {
		scope := registry.Metadata{
			"from_scope": "scope_value",
			"override":   "scope_wins",
		}

		instance, err := instantiator.Instantiate(ctx, bindingID, scope)
		require.NoError(t, err)

		// Create context with existing values
		existing := ctxapi.NewContexter[any]()
		existing.SetValue("from_existing", "existing_value")
		existing.SetValue("override", "existing_value")
		callCtx := context.WithValue(ctx, ctxapi.ValuesCtx, existing)

		resultChan, err := instance.Call(callCtx, "captureMethod", payload.Payloads{})
		require.NoError(t, err)

		result := <-resultChan
		captured := result.Value.Data().(map[string]interface{})

		// Should have both existing and scope values, with scope winning conflicts
		assert.Equal(t, "existing_value", captured["from_existing"])
		assert.Equal(t, "scope_value", captured["from_scope"])
		assert.Equal(t, "scope_wins", captured["override"])
	})
}
