package contract

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/internal/uniqid"
	functionSys "github.com/wippyai/runtime/system/function"
	"github.com/wippyai/runtime/system/relay"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func setupInstantiatorTest() (*Instantiator, event.Bus, *Registry, *functionSys.Registry) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	contractRegistry := NewContractRegistry(bus, logger)
	ctx := ctxapi.NewRootContext()

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	_ = process.WithPIDGenerator(ctx, pidGen)

	functionRegistry := functionSys.NewFunctionRegistry(bus, logger)

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
	ctx := ctxapi.NewRootContext()
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
	contractID := registry.NewID("test", "my_contract")
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
	bindingID := registry.NewID("test", "my_binding")
	testBinding := &contract.Binding{
		Meta: attrs.Bag{"version": "1.0"},
		Contracts: []contract.BoundContract{
			{
				Contract:        contractID,
				Methods:         map[string]registry.ID{"testMethod": registry.NewID("test", "test_func")},
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
	scope := attrs.Bag{"required_key": "value"}
	instance, err := instantiator.Instantiate(ctx, bindingID, scope)
	require.NoError(t, err)
	assert.Len(t, instance.Implements(), 1)

	// Test with non-existent binding
	_, err = instantiator.Instantiate(ctx, registry.NewID("test", "missing"), attrs.Bag{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contract binding 'test:missing' not found")

	// Test with nil context - should succeed
	instanceNil, err := instantiator.Instantiate(ctx, bindingID, nil)
	require.NoError(t, err)
	assert.NotNil(t, instanceNil)

	// Test with empty context - should succeed
	instanceEmpty, err := instantiator.Instantiate(ctx, bindingID, attrs.Bag{})
	require.NoError(t, err)
	assert.NotNil(t, instanceEmpty)
}

func TestInstanceImpl_ScopeValidation(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
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
	contractID := registry.NewID("test", "scope_contract")
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
		instanceScope attrs.Bag
		expectedError string
	}{
		{
			name:          "no context required",
			scopeRequired: []string{},
			instanceScope: attrs.Bag{},
		},
		{
			name:          "required context present",
			scopeRequired: []string{"key1", "key2"},
			instanceScope: attrs.Bag{"key1": "value1", "key2": "value2"},
		},
		{
			name:          "missing one required key",
			scopeRequired: []string{"key1", "key2"},
			instanceScope: attrs.Bag{"key1": "value1"},
			expectedError: "missing required context keys: [key2]",
		},
		{
			name:          "missing all required keys",
			scopeRequired: []string{"key1", "key2"},
			instanceScope: attrs.Bag{},
			expectedError: "missing required context keys: [key1, key2]",
		},
		{
			name:          "nil context with required keys",
			scopeRequired: []string{"key1"},
			instanceScope: nil,
			expectedError: "missing required context keys: [key1]",
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bindingID := registry.ID{NS: "test", Name: fmt.Sprintf("binding_%d", i)}
			testBinding := &contract.Binding{
				Contracts: []contract.BoundContract{
					{
						Contract:        contractID,
						Methods:         map[string]registry.ID{"validateMethod": registry.NewID("test", "dummy_func")},
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
	ctx := ctxapi.NewRootContext()
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

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

	// Subscribe to contract events
	contractSub, err := eventbus.NewSubscriber(ctx, bus, contract.System, "contract.*", func(evt event.Event) {
		if evt.Kind == contract.Accept {
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer contractSub.Close()

	// Subscribe to function events
	functionSub, err := eventbus.NewSubscriber(ctx, bus, function.System, "function.*", func(evt event.Event) {
		if evt.Kind == function.Accept {
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer functionSub.Close()

	// Register function
	funcID := registry.NewID("test", "test_func")
	testFunc := function.Func(func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{Value: payload.New("function_result")}, nil
	})

	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data: &function.FuncEntry{
			Handler: testFunc,
			Options: nil,
		},
	})
	wg.Wait()

	// Register contract
	contractID := registry.NewID("test", "my_contract")
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
	bindingID := registry.NewID("test", "my_binding")
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
	instance, err := instantiator.Instantiate(ctx, bindingID, attrs.Bag{})
	require.NoError(t, err)

	result, err := instance.Call(ctx, "testMethod", payload.Payloads{payload.New("test_input")})
	require.NoError(t, err)

	assert.Equal(t, "function_result", result.Value.Data().(string))

	// Test method not bound
	_, err = instance.Call(ctx, "unknownMethod", payload.Payloads{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "method 'unknownMethod' not bound")
}

func TestInstanceImpl_ContextMerging(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

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

	// Subscribe to function events for waiting on registration
	funcSub, err := eventbus.NewSubscriber(ctx, bus, function.System, "function.*", func(evt event.Event) {
		if evt.Kind == function.Accept {
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer funcSub.Close()

	// Function that checks context values
	funcID := registry.NewID("test", "context_func")
	testFunc := function.Func(func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
		result := map[string]interface{}{"has_context": false}
		if values := ctxapi.GetValues(ctx); values != nil {
			existing, existingOk := values.Get("existing")
			scope, scopeOk := values.Get("context")
			override, overrideOk := values.Get("override")

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

		return &runtime.Result{Value: payload.New(result)}, nil
	})

	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data: &function.FuncEntry{
			Handler: testFunc,
			Options: nil,
		},
	})
	wg.Wait()

	// Register contract and binding
	contractID := registry.NewID("test", "context_contract")
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

	bindingID := registry.NewID("test", "context_binding")
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

	// Test with nil context
	instanceNil, err := instantiator.Instantiate(ctx, bindingID, nil)
	require.NoError(t, err)

	result, err := instanceNil.Call(ctx, "contextMethod", payload.Payloads{})
	require.NoError(t, err)

	values := result.Value.Data().(map[string]interface{})
	assert.False(t, values["has_context"].(bool))

	// Test context merging - context values should be merged with existing context
	scope := attrs.Bag{
		"context":  "from_scope",
		"override": "from_scope",
	}
	instance, err := instantiator.Instantiate(ctx, bindingID, scope)
	require.NoError(t, err)

	callCtx, _ := ctxapi.OpenFrameContext(ctx)
	existing := ctxapi.NewValues()
	existing.Set("existing", "from_existing")
	existing.Set("override", "from_existing")
	err = ctxapi.SetValues(callCtx, existing)
	require.NoError(t, err)

	result, err = instance.Call(callCtx, "contextMethod", payload.Payloads{})
	require.NoError(t, err)

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
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

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
	funcID := registry.NewID("test", "capture_context_func")
	testFunc := function.Func(func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
		captured := map[string]interface{}{}
		if values := ctxapi.GetValues(ctx); values != nil {
			// Capture all values from the context
			values.Iterate(func(key string, value any) {
				captured[key] = value
			})
		}

		return &runtime.Result{Value: payload.New(captured)}, nil
	})

	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data: &function.FuncEntry{
			Handler: testFunc,
			Options: nil,
		},
	})

	// Register contract and binding
	contractID := registry.NewID("test", "capture_contract")
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

	bindingID := registry.NewID("test", "capture_binding")
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

	t.Run("empty context produces no context", func(t *testing.T) {
		instance, err := instantiator.Instantiate(ctx, bindingID, attrs.Bag{})
		require.NoError(t, err)

		result, err := instance.Call(ctx, "captureMethod", payload.Payloads{})
		require.NoError(t, err)

		captured := result.Value.Data().(map[string]interface{})

		// Should be empty since no context was provided and no existing context
		assert.Empty(t, captured)
	})

	t.Run("context values are properly passed to function", func(t *testing.T) {
		scope := attrs.Bag{
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

		result, err := instance.Call(ctx, "captureMethod", payload.Payloads{})
		require.NoError(t, err)

		captured := result.Value.Data().(map[string]interface{})

		// All context values should be present in the context
		assert.Equal(t, "test_app", captured["app_name"])
		assert.Equal(t, "1.0.0", captured["version"])
		assert.Equal(t, "test", captured["environment"])
		assert.Contains(t, captured, "feature_flags")
	})

	t.Run("context merges with existing context", func(t *testing.T) {
		scope := attrs.Bag{
			"from_scope": "scope_value",
			"override":   "scope_wins",
		}

		instance, err := instantiator.Instantiate(ctx, bindingID, scope)
		require.NoError(t, err)

		// Create context with existing values
		callCtx, _ := ctxapi.OpenFrameContext(ctx)
		existing := ctxapi.NewValues()
		existing.Set("from_existing", "existing_value")
		existing.Set("override", "existing_value")
		err = ctxapi.SetValues(callCtx, existing)
		require.NoError(t, err)

		result, err := instance.Call(callCtx, "captureMethod", payload.Payloads{})
		require.NoError(t, err)

		captured := result.Value.Data().(map[string]interface{})

		// Should have both existing and context values, with context winning conflicts
		assert.Equal(t, "existing_value", captured["from_existing"])
		assert.Equal(t, "scope_value", captured["from_scope"])
		assert.Equal(t, "scope_wins", captured["override"])
	})
}

// TestInstanceImpl_ContextValidationIssue demonstrates that the context validation fix works
// The fix allows required context keys to be found in EITHER scope OR Go context
func TestInstanceImpl_ContextValidationIssue(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

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

	// Subscribe to function events for waiting on registration
	funcSub, err := eventbus.NewSubscriber(ctx, bus, function.System, "function.*", func(evt event.Event) {
		if evt.Kind == function.Accept {
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer funcSub.Close()

	// Register function that returns a test result
	funcID := registry.NewID("test", "context_test_func")
	testFunc := function.Func(func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{Value: payload.New("validation_and_execution_success")}, nil
	})

	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data: &function.FuncEntry{
			Handler: testFunc,
			Options: nil,
		},
	})
	wg.Wait()

	// Register contract that requires origin_id
	contractID := registry.NewID("test", "context_validation_contract")
	testDef := &contract.Definition{
		Methods: []contract.MethodDef{{Name: "requiresOriginId"}},
	}

	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterDefinition,
		Path:   contractID.String(),
		Data:   testDef,
	})
	wg.Wait()

	// Register binding that requires origin_id in context
	bindingID := registry.NewID("test", "context_validation_binding")
	testBinding := &contract.Binding{
		Contracts: []contract.BoundContract{
			{
				Contract:        contractID,
				Methods:         map[string]registry.ID{"requiresOriginId": funcID},
				ContextRequired: []string{"origin_id"}, // THIS IS THE KEY - requires origin_id
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

	t.Run("FIXED: validation now passes when origin_id is in Go context but not scope", func(t *testing.T) {
		// Create Go context with origin_id present using Values
		callCtx, fc := ctxapi.OpenFrameContext(ctx)
		values := ctxapi.NewValues()
		values.Set("origin_id", "test-uuid-123")
		values.Set("other_key", "other_value")
		_ = fc.Set(ctxapi.ValuesCtx, values)

		// Create instance with EMPTY scope (no origin_id in scope parameter)
		instance, err := instantiator.Instantiate(callCtx, bindingID, attrs.Bag{})
		require.NoError(t, err, "Instantiation should succeed")

		// Try to call method - this should NOW SUCCEED validation with the fix
		result, err := instance.Call(callCtx, "requiresOriginId", payload.Payloads{})
		require.NoError(t, err, "Call should succeed - validation finds origin_id in Go context")

		// Function should execute and return result
		assert.Equal(t, "validation_and_execution_success", result.Value.Data().(string))
	})

	t.Run("validation passes when origin_id is in scope parameter", func(t *testing.T) {
		// Create Go context (may or may not have origin_id)
		callCtx := ctx

		// Create instance with origin_id in scope parameter
		scope := attrs.Bag{"origin_id": "test-uuid-456"}
		instance, err := instantiator.Instantiate(callCtx, bindingID, scope)
		require.NoError(t, err, "Instantiation should succeed")

		// Try to call method - should succeed
		result, err := instance.Call(callCtx, "requiresOriginId", payload.Payloads{})
		require.NoError(t, err, "Call should succeed - validation finds origin_id in scope")

		// Function should execute and return result
		assert.Equal(t, "validation_and_execution_success", result.Value.Data().(string))
	})

	t.Run("validation passes when origin_id is in both Go context AND scope", func(t *testing.T) {
		// Create Go context with origin_id using Values
		callCtx, fc := ctxapi.OpenFrameContext(ctx)
		values := ctxapi.NewValues()
		values.Set("origin_id", "from-go-context")
		_ = fc.Set(ctxapi.ValuesCtx, values)

		// Create instance with origin_id in scope (should override Go context value)
		scope := attrs.Bag{"origin_id": "from-scope"}
		instance, err := instantiator.Instantiate(callCtx, bindingID, scope)
		require.NoError(t, err, "Instantiation should succeed")

		// Try to call method - should succeed
		result, err := instance.Call(callCtx, "requiresOriginId", payload.Payloads{})
		require.NoError(t, err, "Call should succeed - validation finds origin_id in both places")

		// Function should execute and return result
		assert.Equal(t, "validation_and_execution_success", result.Value.Data().(string))
	})

	t.Run("validation still fails when origin_id is missing from both scope and Go context", func(t *testing.T) {
		// Create fresh Go context without origin_id (don't reuse ctx which may have values)
		freshCtx := ctxapi.NewRootContext()
		callCtx, _ := ctxapi.OpenFrameContext(freshCtx)

		// Create instance with empty scope
		instance, err := instantiator.Instantiate(callCtx, bindingID, attrs.Bag{})
		require.NoError(t, err, "Instantiation should succeed")

		// Try to call method - should fail validation
		_, err = instance.Call(callCtx, "requiresOriginId", payload.Payloads{})
		require.Error(t, err, "Call should fail when origin_id is missing from both places")
		assert.Contains(t, err.Error(), "missing required context keys: [origin_id]")
	})
}
