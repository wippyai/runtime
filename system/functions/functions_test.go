package functions

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/runtime"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupTest() (*FunctionRegistry, events.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	executor := NewExecutor(bus, logger)
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
	executor, bus := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	tests := []struct {
		name string
		evt  events.Event
	}{
		{
			name: "invalid register handler data",
			evt: events.Event{
				System: runtime.FunctionSystem,
				Kind:   runtime.RegisterFunctionHandler,
				Path:   "test.handler",
				Data:   "invalid data",
			},
		},
		{
			name: "invalid delete handler data",
			evt: events.Event{
				System: runtime.FunctionSystem,
				Kind:   runtime.DeleteFunctionHandler,
				Path:   "test.handler",
				Data:   "invalid data",
			},
		},
		{
			name: "unknown event kind",
			evt: events.Event{
				System: runtime.FunctionSystem,
				Kind:   "unknown.event",
				Data:   nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify no panic occurs
			bus.Send(ctx, tt.evt)
			time.Sleep(1 * time.Millisecond)
		})
	}
}

func TestFunctions_EventResponses(t *testing.T) {
	ctx := context.Background()
	executor, bus := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	// Spawn a subscriber to listen for Accept/Reject events
	var responses []events.Event
	var mu sync.Mutex
	var wg sync.WaitGroup

	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		runtime.FunctionSystem,
		"functions.*",
		func(evt events.Event) {
			if evt.Kind == runtime.AcceptFunction || evt.Kind == runtime.RejectFunction {
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
		event        events.Event
		expectedKind events.Kind
		expectedPath events.Path
	}{
		{
			name: "valid function registration",
			event: events.Event{
				System: runtime.FunctionSystem,
				Kind:   runtime.RegisterFunctionHandler,
				Path:   "default:test.handler",
				Data: runtime.Func(func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
					return make(chan *runtime.Result), nil
				}),
			},
			expectedKind: runtime.AcceptFunction,
			expectedPath: "default:test.handler",
		},
		{
			name: "invalid function registration",
			event: events.Event{
				System: runtime.FunctionSystem,
				Kind:   runtime.RegisterFunctionHandler,
				Path:   "invalid:handler",
				Data:   "not a function",
			},
			expectedKind: runtime.RejectFunction,
			expectedPath: "invalid:handler",
		},
		{
			name: "delete existing function",
			event: events.Event{
				System: runtime.FunctionSystem,
				Kind:   runtime.DeleteFunctionHandler,
				Path:   "default:test.handler",
			},
			expectedKind: runtime.AcceptFunction,
			expectedPath: "default:test.handler",
		},
		{
			name: "delete non-existent function",
			event: events.Event{
				System: runtime.FunctionSystem,
				Kind:   runtime.DeleteFunctionHandler,
				Path:   "nonexistent:handler",
			},
			expectedKind: runtime.RejectFunction,
			expectedPath: "nonexistent:handler",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responses = nil // Clear previous responses
			wg.Add(1)       // Expect one response event

			// Send the test event
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

			assert.Equal(t, runtime.FunctionSystem, lastResponse.System)
			assert.Equal(t, tt.expectedKind, lastResponse.Kind)
			assert.Equal(t, tt.expectedPath, lastResponse.Path)
		})
	}
}

func TestFunctions_Execute(t *testing.T) {
	ctx := context.Background()
	executor, bus := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	// Add response tracking for registration events
	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		runtime.FunctionSystem,
		"functions.*",
		func(evt events.Event) {
			if evt.Kind == runtime.AcceptFunction {
				wg.Done()
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	tests := []struct {
		name          string
		setupHandler  func(bus events.Bus, wg *sync.WaitGroup)
		task          runtime.Task
		expectedErr   string
		expectedValue string
	}{
		{
			name: "successful execution",
			setupHandler: func(bus events.Bus, wg *sync.WaitGroup) {
				target := registry.ID{NS: "test", Name: "handler"}
				handler := func(ctx context.Context, _ runtime.Task) (chan *runtime.Result, error) {
					resultChan := make(chan *runtime.Result, 1)
					resultChan <- &runtime.Result{
						Payload: payload.New("success"),
					}
					close(resultChan)
					return resultChan, nil
				}

				wg.Add(1) // Wait for registration acceptance
				bus.Send(ctx, events.Event{
					System: runtime.FunctionSystem,
					Kind:   runtime.RegisterFunctionHandler,
					Path:   target.String(),
					Data:   runtime.Func(handler),
				})
			},
			task: runtime.Task{
				Handler:  registry.ID{NS: "test", Name: "handler"},
				Payloads: []payload.Payload{payload.New("test input")},
			},
			expectedValue: "success",
		},
		{
			name: "handler not found",
			task: runtime.Task{
				Handler:  registry.ID{NS: "nonexistent", Name: "handler"},
				Payloads: []payload.Payload{payload.New("test input")},
			},
			expectedErr: "no handler registered for target: nonexistent:handler",
		},
		{
			name: "handler returns error",
			setupHandler: func(bus events.Bus, wg *sync.WaitGroup) {
				target := registry.ID{NS: "error", Name: "handler"}
				handler := func(ctx context.Context, _ runtime.Task) (chan *runtime.Result, error) {
					return nil, fmt.Errorf("handler error")
				}

				wg.Add(1) // Wait for registration acceptance
				bus.Send(ctx, events.Event{
					System: runtime.FunctionSystem,
					Kind:   runtime.RegisterFunctionHandler,
					Path:   target.String(),
					Data:   runtime.Func(handler),
				})
			},
			task: runtime.Task{
				Handler:  registry.ID{NS: "error", Name: "handler"},
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

			resultChan, err := executor.Call(context.Background(), tt.task)

			if tt.expectedErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resultChan)

			result := <-resultChan
			require.NotNil(t, result)
			assert.Equal(t, tt.expectedValue, result.Payload.Data().(string))
		})
	}
}

func TestFunctions_ConcurrentHandlerRegistration(t *testing.T) {
	ctx := context.Background()
	executor, bus := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	const numHandlers = 10
	var wg sync.WaitGroup

	// Add response tracking for registration events
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		runtime.FunctionSystem,
		"functions.*",
		func(evt events.Event) {
			if evt.Kind == runtime.AcceptFunction {
				wg.Done()
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	// Register handlers concurrently
	for i := 0; i < numHandlers; i++ {
		wg.Add(1) // Add before launching goroutine
		go func(idx int) {
			target := registry.ID{
				NS:   "test",
				Name: fmt.Sprintf("handler.%d", idx),
			}

			handler := func(ctx context.Context, _ runtime.Task) (chan *runtime.Result, error) {
				resultChan := make(chan *runtime.Result, 1)
				resultChan <- &runtime.Result{
					Payload: payload.New(fmt.Sprintf("result %d", idx)),
				}
				close(resultChan)
				return resultChan, nil
			}

			bus.Send(ctx, events.Event{
				System: runtime.FunctionSystem,
				Kind:   runtime.RegisterFunctionHandler,
				Path:   target.String(),
				Data:   runtime.Func(handler),
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
		resultChan, err := executor.Call(context.Background(), runtime.Task{
			Handler:  target,
			Payloads: []payload.Payload{payload.New("test")},
		})
		require.NoError(t, err)
		result := <-resultChan
		assert.Equal(t, fmt.Sprintf("result %d", i), result.Payload.Data().(string))
	}
}
