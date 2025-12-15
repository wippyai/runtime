package function

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/process"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/system/relay"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func setupTest() (*Registry, event.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	executor := NewFunctionRegistry(bus, logger)
	return executor, bus
}

// Keep working test unchanged
func TestFunctions_StartStop(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	executor, _ := setupTest()

	err := executor.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, executor.subscriber)

	err = executor.Stop()
	require.NoError(t, err)
}

// Keep working test unchanged
func TestFunctions_InvalidEvents(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))

	executor, bus := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	tests := []struct {
		name string
		evt  event.Event
	}{
		{
			name: "invalid register handler data",
			evt: event.Event{
				System: function.System,
				Kind:   function.Register,
				Path:   "test.handler",
				Data:   "invalid data",
			},
		},
		{
			name: "invalid delete handler data",
			evt: event.Event{
				System: function.System,
				Kind:   function.Delete,
				Path:   "test.handler",
				Data:   "invalid data",
			},
		},
		{
			name: "unknown event kind",
			evt: event.Event{
				System: function.System,
				Kind:   "unknown.event",
				Data:   nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			// Just verify no panic occurs
			bus.Send(ctx, tt.evt)
			time.Sleep(1 * time.Millisecond)
		})
	}
}

func TestFunctions_EventResponses(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))

	executor, bus := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	// Spawn a subscriber to listen for Accept/Reject events
	var responses []event.Event
	var mu sync.Mutex
	var wg sync.WaitGroup

	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		function.System,
		"function.*",
		func(evt event.Event) {
			if evt.Kind == function.Accept || evt.Kind == function.Reject {
				mu.Lock()
				responses = append(responses, evt)
				mu.Unlock()
				wg.Done()
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	tests := []struct {
		name         string
		event        event.Event
		expectedKind event.Kind
		expectedPath event.Path
	}{
		{
			name: "valid function registration",
			event: event.Event{
				System: function.System,
				Kind:   function.Register,
				Path:   "default:test.handler",
				Data: &function.FuncEntry{
					Handler: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
						return &runtime.Result{}, nil
					},
					Options: nil,
				},
			},
			expectedKind: function.Accept,
			expectedPath: "default:test.handler",
		},
		{
			name: "invalid function registration",
			event: event.Event{
				System: function.System,
				Kind:   function.Register,
				Path:   "invalid:handler",
				Data:   "not a function",
			},
			expectedKind: function.Reject,
			expectedPath: "invalid:handler",
		},
		{
			name: "delete existing function",
			event: event.Event{
				System: function.System,
				Kind:   function.Delete,
				Path:   "default:test.handler",
			},
			expectedKind: function.Accept,
			expectedPath: "default:test.handler",
		},
		{
			name: "delete non-existent function",
			event: event.Event{
				System: function.System,
				Kind:   function.Delete,
				Path:   "nonexistent:handler",
			},
			expectedKind: function.Reject,
			expectedPath: "nonexistent:handler",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responses = nil // Clear previous responses
			wg.Add(1)       // Expect one response event

			// send the test event
			bus.Send(ctx, tt.event)

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

			assert.Equal(t, function.System, lastResponse.System)
			assert.Equal(t, tt.expectedKind, lastResponse.Kind)
			assert.Equal(t, tt.expectedPath, lastResponse.Path)
		})
	}
}

func TestFunctions_Execute(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))

	// Add PID generator
	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	executor, bus := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	// AddCleanup response tracking for registration events
	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		function.System,
		"function.*",
		func(evt event.Event) {
			if evt.Kind == function.Accept {
				wg.Done()
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	tests := []struct {
		name          string
		setupHandler  func(bus event.Bus, wg *sync.WaitGroup)
		task          runtime.Task
		expectedErr   string
		expectedValue string
	}{
		{
			name: "successful execution",
			setupHandler: func(bus event.Bus, wg *sync.WaitGroup) {
				target := registry.NewID("test", "handler")
				handler := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
					return &runtime.Result{
						Value: payload.New("success"),
					}, nil
				}

				wg.Add(1) // Wait for registration acceptance
				bus.Send(ctx, event.Event{
					System: function.System,
					Kind:   function.Register,
					Path:   target.String(),
					Data: &function.FuncEntry{
						Handler: handler,
						Options: nil,
					},
				})
			},
			task: runtime.Task{
				ID:       registry.NewID("test", "handler"),
				Payloads: []payload.Payload{payload.New("test input")},
			},
			expectedValue: "success",
		},
		{
			name: "handler not found",
			task: runtime.Task{
				ID:       registry.NewID("nonexistent", "handler"),
				Payloads: []payload.Payload{payload.New("test input")},
			},
			expectedErr: "no handler registered for target: nonexistent:handler",
		},
		{
			name: "handler returns error",
			setupHandler: func(bus event.Bus, wg *sync.WaitGroup) {
				target := registry.NewID("error", "handler")
				handler := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
					return nil, fmt.Errorf("handler error")
				}

				wg.Add(1) // Wait for registration acceptance
				bus.Send(ctx, event.Event{
					System: function.System,
					Kind:   function.Register,
					Path:   target.String(),
					Data: &function.FuncEntry{
						Handler: handler,
						Options: nil,
					},
				})
			},
			task: runtime.Task{
				ID:       registry.NewID("error", "handler"),
				Payloads: []payload.Payload{payload.New("test input")},
			},
			expectedErr: "handler error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupHandler != nil {
				tt.setupHandler(bus, &wg)
				wg.Wait() // Wait for handler registration to complete
			}

			result, err := executor.Call(ctx, tt.task)

			if tt.expectedErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tt.expectedValue, result.Value.Data().(string))
		})
	}
}

func TestFunctions_ConcurrentHandlerRegistration(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	executor, bus := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	const numHandlers = 10
	var wg sync.WaitGroup

	// AddCleanup response tracking for registration events
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		function.System,
		"function.*",
		func(evt event.Event) {
			if evt.Kind == function.Accept {
				wg.Done()
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	// Register handlers concurrently
	for i := 0; i < numHandlers; i++ {
		wg.Add(1) // AddCleanup before launching goroutine
		go func(idx int) {
			target := registry.NewID("test", fmt.Sprintf("handler.%d", idx))

			handler := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
				return &runtime.Result{
					Value: payload.New(fmt.Sprintf("result %d", idx)),
				}, nil
			}

			bus.Send(ctx, event.Event{
				System: function.System,
				Kind:   function.Register,
				Path:   target.String(),
				Data: &function.FuncEntry{
					Handler: handler,
					Options: nil,
				},
			})
		}(i)
	}

	// Wait for all registrations to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - continue with checks
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for handler registrations")
	}

	var count int
	executor.handlers.Range(func(_, _ any) bool {
		count++
		return true
	})
	assert.Equal(t, numHandlers, count)

	// Test executing all handlers
	for i := 0; i < numHandlers; i++ {
		target := registry.NewID("test", fmt.Sprintf("handler.%d", i))
		result, err := executor.Call(ctx, runtime.Task{
			ID:       target,
			Payloads: []payload.Payload{payload.New("test")},
		})
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("result %d", i), result.Value.Data().(string))
	}
}

func TestFunctions_CallErrorHandling(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	executor, _ := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	tests := []struct {
		name        string
		task        runtime.Task
		expectedErr string
	}{
		{
			name:        "non-existent handler",
			task:        runtime.Task{ID: registry.ParseID("nonexistent:handler")},
			expectedErr: "no handler registered for target: nonexistent:handler",
		},
		{
			name:        "invalid handler type",
			task:        runtime.Task{ID: registry.ParseID("invalid:handler")},
			expectedErr: "invalid handler type for target: invalid:handler",
		},
	}

	// Register an invalid handler type for the second test
	executor.handlers.Store(registry.ParseID("invalid:handler"), "not a function")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := executor.Call(ctx, tt.task)
			require.Error(t, err)
			assert.Equal(t, tt.expectedErr, err.Error())
			assert.Nil(t, ch)
		})
	}
}

func TestFunctions_CallNilContext(t *testing.T) {
	executor, _ := setupTest()

	//nolint:staticcheck // deliberately testing nil context handling
	result, err := executor.Call(nil, runtime.Task{ID: registry.NewID("test", "func")})
	assert.ErrorIs(t, err, function.ErrNilContext)
	assert.Nil(t, result)
}

func TestFunctions_CallNoPIDGenerator(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))
	// No PID generator set

	executor, _ := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	// Register a valid handler
	handlerCalled := false
	handlerID := registry.NewID("test", "func")
	executor.handlers.Store(handlerID, function.Func(func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		handlerCalled = true
		return &runtime.Result{}, nil
	}))

	_, err := executor.Call(ctx, runtime.Task{ID: handlerID})
	assert.ErrorIs(t, err, function.ErrPIDGeneratorNotFound)
	assert.False(t, handlerCalled, "handler should not be called when PID generator is missing")
}

func TestFunctions_FrameContextHandling(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))

	// Add PID generator to context
	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	executor, _ := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	// Register a handler that checks context
	handlerID := registry.ParseID("test:context-handler")
	executor.handlers.Store(handlerID, function.Func(func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
		// Verify context has required values
		pid, exists := runtime.GetFramePID(ctx)
		require.True(t, exists)
		require.NotEmpty(t, pid.UniqID)
		// Host should be the function ID (each function is its own mini-host)
		assert.Equal(t, task.ID.String(), pid.Host)
		return &runtime.Result{}, nil
	}))

	t.Run("nil context", func(t *testing.T) {
		result, err := executor.Call(ctx, runtime.Task{ID: handlerID})
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("context with values", func(t *testing.T) {
		result, err := executor.Call(ctx, runtime.Task{ID: handlerID})
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
}

func TestFunctions_EdgeCases(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	executor, bus := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	t.Run("register with empty path", func(t *testing.T) {
		bus.Send(ctx, event.Event{
			System: function.System,
			Kind:   function.Register,
			Path:   "",
			Data: &function.FuncEntry{
				Handler: func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
					return &runtime.Result{}, nil
				},
				Options: nil,
			},
		})

		// Wait for registration to complete
		time.Sleep(100 * time.Millisecond)

		// Verify function was registered
		_, exists := executor.handlers.Load(registry.ParseID(""))
		assert.True(t, exists)
	})

	t.Run("register nil function", func(t *testing.T) {
		bus.Send(ctx, event.Event{
			System: function.System,
			Kind:   function.Register,
			Path:   "test:nil-function",
			Data:   nil,
		})

		// Wait for registration to complete
		time.Sleep(100 * time.Millisecond)

		// Verify function was not registered
		_, exists := executor.handlers.Load(registry.ParseID("test:nil-function"))
		assert.False(t, exists)
	})

	t.Run("delete empty path", func(t *testing.T) {
		bus.Send(ctx, event.Event{
			System: function.System,
			Kind:   function.Delete,
			Path:   "",
		})

		// Wait for deletion to complete
		time.Sleep(100 * time.Millisecond)

		// Verify function was deleted
		_, exists := executor.handlers.Load(registry.ParseID(""))
		assert.False(t, exists)
	})
}

func TestFunctions_ConcurrentExecution(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	executor, _ := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Register a handler that returns a result
	handlerID := registry.ParseID("test:concurrent-handler")
	executor.handlers.Store(handlerID, function.Func(func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{
			Value: payload.New(task.ID.String()),
		}, nil
	}))

	// Test concurrent execution
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			result, err := executor.Call(ctx, runtime.Task{ID: handlerID})
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, handlerID.String(), result.Value.Data())
		}()
	}

	wg.Wait()
}

// TestFunctions_ContextInheritance_NestedCalls tests that context values are inherited
// when function A calls function B. This is the core scenario from the bug report:
// - Function A is called with context values
// - Function A calls Function B (without explicit context passing)
// - Function B should still see the context values from A
func TestFunctions_ContextInheritance_NestedCalls(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	executor, _ := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	// Make executor available via context for nested calls
	ctx = function.WithRegistry(ctx, executor)

	// Channel to capture what function B sees
	resultCh := make(chan map[string]any, 1)

	// Register function B - reads context values and reports them
	funcBID := registry.ParseID("test:func-b")
	executor.handlers.Store(funcBID, function.Func(func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
		values := ctxapi.GetValues(ctx)
		result := make(map[string]any)
		if values != nil {
			if v, ok := values.Get("request_id"); ok {
				result["request_id"] = v
			}
			if v, ok := values.Get("user_id"); ok {
				result["user_id"] = v
			}
		}
		resultCh <- result
		return &runtime.Result{Value: payload.New("ok")}, nil
	}))

	// Register function A - sets context values, then calls function B
	funcAID := registry.ParseID("test:func-a")
	executor.handlers.Store(funcAID, function.Func(func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
		// Call function B without explicit context - it should inherit from A's context
		_, err := executor.Call(ctx, runtime.Task{ID: funcBID})
		return &runtime.Result{Value: payload.New("done")}, err
	}))

	// Call function A with context values
	values := ctxapi.NewValues()
	values.Set("request_id", "req-123")
	values.Set("user_id", 42)

	task := runtime.Task{
		ID:      funcAID,
		Context: []ctxapi.Pair{ctxapi.ValuesPair(values)},
	}

	result, err := executor.Call(ctx, task)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check what function B received
	select {
	case received := <-resultCh:
		assert.Equal(t, "req-123", received["request_id"], "request_id should be inherited")
		assert.Equal(t, 42, received["user_id"], "user_id should be inherited")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for function B result")
	}
}

// TestFunctions_ContextInheritance_EmptyTaskContext tests the scenario where
// a function is called with Task.Context = nil (simulating funcs.new():call() without with_context()).
// The nested call should still inherit context values from the parent frame.
func TestFunctions_ContextInheritance_EmptyTaskContext(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	executor, _ := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	ctx = function.WithRegistry(ctx, executor)

	resultCh := make(chan string, 1)

	// Function B - reads context values
	funcBID := registry.ParseID("test:func-b")
	executor.handlers.Store(funcBID, function.Func(func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
		values := ctxapi.GetValues(ctx)
		if values == nil {
			resultCh <- "no values"
			return &runtime.Result{}, nil
		}
		if v, ok := values.Get("trace_id"); ok {
			resultCh <- v.(string)
		} else {
			resultCh <- "trace_id not found"
		}
		return &runtime.Result{}, nil
	}))

	// Function A - calls B with EMPTY Task.Context (simulates funcs.new():call() without with_context())
	funcAID := registry.ParseID("test:func-a")
	executor.handlers.Store(funcAID, function.Func(func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
		// Call B with empty Task.Context - this simulates funcs.new():call() behavior
		_, err := executor.Call(ctx, runtime.Task{
			ID:      funcBID,
			Context: nil, // No explicit context - should still inherit from parent
		})
		return &runtime.Result{}, err
	}))

	// Call A with context values
	values := ctxapi.NewValues()
	values.Set("trace_id", "trace-xyz-789")

	task := runtime.Task{
		ID:      funcAID,
		Context: []ctxapi.Pair{ctxapi.ValuesPair(values)},
	}

	_, err := executor.Call(ctx, task)
	require.NoError(t, err)

	select {
	case received := <-resultCh:
		assert.Equal(t, "trace-xyz-789", received, "trace_id should be inherited even with empty Task.Context")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for function B result")
	}
}

// TestFunctions_ContextInheritance_ThreeLevels tests context inheritance through
// 3 levels of nested function calls: A -> B -> C
func TestFunctions_ContextInheritance_ThreeLevels(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = relayapi.WithNode(ctx, relay.NewNode("test"))

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	executor, _ := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	ctx = function.WithRegistry(ctx, executor)

	resultCh := make(chan string, 1)

	// Function C - deepest level, reads context
	funcCID := registry.ParseID("test:func-c")
	executor.handlers.Store(funcCID, function.Func(func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
		values := ctxapi.GetValues(ctx)
		if values == nil {
			resultCh <- "no values"
			return &runtime.Result{}, nil
		}
		if v, ok := values.Get("trace_id"); ok {
			resultCh <- v.(string)
		} else {
			resultCh <- "trace_id not found"
		}
		return &runtime.Result{}, nil
	}))

	// Function B - middle level, calls C
	funcBID := registry.ParseID("test:func-b")
	executor.handlers.Store(funcBID, function.Func(func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
		_, err := executor.Call(ctx, runtime.Task{ID: funcCID})
		return &runtime.Result{}, err
	}))

	// Function A - top level, calls B
	funcAID := registry.ParseID("test:func-a")
	executor.handlers.Store(funcAID, function.Func(func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
		_, err := executor.Call(ctx, runtime.Task{ID: funcBID})
		return &runtime.Result{}, err
	}))

	// Call A with context
	values := ctxapi.NewValues()
	values.Set("trace_id", "trace-abc-123")

	task := runtime.Task{
		ID:      funcAID,
		Context: []ctxapi.Pair{ctxapi.ValuesPair(values)},
	}

	_, err := executor.Call(ctx, task)
	require.NoError(t, err)

	select {
	case received := <-resultCh:
		assert.Equal(t, "trace-abc-123", received, "trace_id should propagate through 3 levels")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for function C result")
	}
}
