package function

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/function"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/system/pubsub"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupTest() (*Registry, event.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	executor := NewFunctionRegistry(bus, pubsub.NewHost(context.Background(), pubsub.HostConfig{
		BufferSize: 100,
	}), logger)
	return executor, bus
}

// Keep working test unchanged
func TestFunctions_StartStop(t *testing.T) {
	ctx := context.Background()
	executor, _ := setupTest()

	err := executor.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, executor.subscriber)

	err = executor.Stop()
	require.NoError(t, err)
}

// Keep working test unchanged
func TestFunctions_InvalidEvents(t *testing.T) {
	ctx := context.Background()
	ctx = pubsubapi.WithNode(ctx, pubsub.NewNode("test"))

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
	ctx := context.Background()
	ctx = pubsubapi.WithNode(ctx, pubsub.NewNode("test"))

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
				Data: function.Func(func(_ context.Context, _ runtime.Task) (chan *runtime.Result, error) {
					return make(chan *runtime.Result), nil
				}),
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
	ctx := context.Background()
	ctx = pubsubapi.WithNode(ctx, pubsub.NewNode("test"))

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
				target := registry.ID{NS: "test", Name: "handler"}
				handler := func(_ context.Context, _ runtime.Task) (chan *runtime.Result, error) {
					resultChan := make(chan *runtime.Result, 1)
					resultChan <- &runtime.Result{
						Value: payload.New("success"),
					}
					close(resultChan)
					return resultChan, nil
				}

				wg.Add(1) // Wait for registration acceptance
				bus.Send(ctx, event.Event{
					System: function.System,
					Kind:   function.Register,
					Path:   target.String(),
					Data:   function.Func(handler),
				})
			},
			task: runtime.Task{
				ID:       registry.ID{NS: "test", Name: "handler"},
				Payloads: []payload.Payload{payload.New("test input")},
			},
			expectedValue: "success",
		},
		{
			name: "handler not found",
			task: runtime.Task{
				ID:       registry.ID{NS: "nonexistent", Name: "handler"},
				Payloads: []payload.Payload{payload.New("test input")},
			},
			expectedErr: "no handler registered for target: nonexistent:handler",
		},
		{
			name: "handler returns error",
			setupHandler: func(bus event.Bus, wg *sync.WaitGroup) {
				target := registry.ID{NS: "error", Name: "handler"}
				handler := func(_ context.Context, _ runtime.Task) (chan *runtime.Result, error) {
					return nil, fmt.Errorf("handler error")
				}

				wg.Add(1) // Wait for registration acceptance
				bus.Send(ctx, event.Event{
					System: function.System,
					Kind:   function.Register,
					Path:   target.String(),
					Data:   function.Func(handler),
				})
			},
			task: runtime.Task{
				ID:       registry.ID{NS: "error", Name: "handler"},
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

			resultChan, err := executor.Call(ctx, tt.task)

			if tt.expectedErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resultChan)

			result := <-resultChan
			require.NotNil(t, result)
			assert.Equal(t, tt.expectedValue, result.Value.Data().(string))
		})
	}
}

func TestFunctions_ConcurrentHandlerRegistration(t *testing.T) {
	ctx := context.Background()
	ctx = pubsubapi.WithNode(ctx, pubsub.NewNode("test"))

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
			target := registry.ID{
				NS:   "test",
				Name: fmt.Sprintf("handler.%d", idx),
			}

			handler := func(_ context.Context, _ runtime.Task) (chan *runtime.Result, error) {
				resultChan := make(chan *runtime.Result, 1)
				resultChan <- &runtime.Result{
					Value: payload.New(fmt.Sprintf("result %d", idx)),
				}
				close(resultChan)
				return resultChan, nil
			}

			bus.Send(ctx, event.Event{
				System: function.System,
				Kind:   function.Register,
				Path:   target.String(),
				Data:   function.Func(handler),
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
		target := registry.ID{
			NS:   "test",
			Name: fmt.Sprintf("handler.%d", i),
		}
		resultChan, err := executor.Call(ctx, runtime.Task{
			ID:       target,
			Payloads: []payload.Payload{payload.New("test")},
		})
		require.NoError(t, err)
		result := <-resultChan
		assert.Equal(t, fmt.Sprintf("result %d", i), result.Value.Data().(string))
	}
}

func TestFunctions_CallErrorHandling(t *testing.T) {
	ctx := context.Background()
	ctx = pubsubapi.WithNode(ctx, pubsub.NewNode("test", nil))

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
			name: "non-existent handler",
			task: runtime.Task{
				ID: registry.ParseID("nonexistent:handler"),
			},
			expectedErr: "no handler registered for target: nonexistent:handler",
		},
		{
			name: "invalid handler type",
			task: runtime.Task{
				ID: registry.ParseID("invalid:handler"),
			},
			expectedErr: "invalid handler type for target: invalid:handler",
		},
	}

	// Register an invalid handler type for the second test
	executor.handlers.Store(registry.ParseID("invalid:handler"), "not a function")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := executor.Call(ctx, tt.task)
			assert.Error(t, err)
			assert.Equal(t, tt.expectedErr, err.Error())
			assert.Nil(t, ch)
		})
	}
}

func TestFunctions_CallContextHandling(t *testing.T) {
	ctx := context.Background()
	ctx = pubsubapi.WithNode(ctx, pubsub.NewNode("test", nil))

	executor, _ := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	// Register a handler that checks context
	handlerID := registry.ParseID("test:context-handler")
	executor.handlers.Store(handlerID, function.Func(func(ctx context.Context, _ runtime.Task) (chan *runtime.Result, error) {
		// Verify context has required values
		pid, exists := pubsubapi.GetPID(ctx)
		require.True(t, exists)
		require.NotNil(t, pid)
		assert.NotEmpty(t, pid.UniqID)
		assert.Equal(t, function.HostID, pid.Host)
		assert.Equal(t, handlerID, pid.ID)
		return make(chan *runtime.Result), nil
	}))

	t.Run("nil context", func(t *testing.T) {
		ch, err := executor.Call(ctx, runtime.Task{ID: handlerID})
		assert.NoError(t, err)
		assert.NotNil(t, ch)
	})

	t.Run("context with values", func(t *testing.T) {
		ch, err := executor.Call(ctx, runtime.Task{ID: handlerID})
		assert.NoError(t, err)
		assert.NotNil(t, ch)
	})
}

func TestFunctions_EdgeCases(t *testing.T) {
	ctx := context.Background()
	ctx = pubsubapi.WithNode(ctx, pubsub.NewNode("test", nil))

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
			Data: function.Func(func(_ context.Context, _ runtime.Task) (chan *runtime.Result, error) {
				return make(chan *runtime.Result), nil
			}),
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
	ctx := context.Background()
	ctx = pubsubapi.WithNode(ctx, pubsub.NewNode("test", nil))

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
	executor.handlers.Store(handlerID, function.Func(func(_ context.Context, task runtime.Task) (chan *runtime.Result, error) {
		ch := make(chan *runtime.Result, 1)
		ch <- &runtime.Result{
			Value: payload.New(task.ID.String()),
		}
		close(ch)
		return ch, nil
	}))

	// Test concurrent execution
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			ch, err := executor.Call(ctx, runtime.Task{ID: handlerID})
			require.NoError(t, err)
			require.NotNil(t, ch)

			select {
			case result := <-ch:
				assert.Equal(t, handlerID.String(), result.Value.Data())
			case <-time.After(time.Second):
				t.Error("timeout waiting for result")
			}
		}()
	}

	wg.Wait()
}
