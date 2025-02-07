package executor

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

func setupTest() (*Executor, events.Bus) {
	logger, _ := zap.NewDevelopment()
	bus := eventbus.NewBus()
	executor := NewExecutor(bus, logger)
	return executor, bus
}

func TestExecutor_StartStop(t *testing.T) {
	ctx := context.Background()
	executor, _ := setupTest()

	// Test Start
	err := executor.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, executor.subscriber)

	// Test stop
	err = executor.Stop()
	require.NoError(t, err)
}

func TestExecutor_HandlerRegistrationOverBus(t *testing.T) {
	ctx := context.Background()
	executor, bus := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	target := registry.ID("test.handler")

	// Create a test handler
	handler := func(_ runtime.Task) (chan *runtime.Result, error) {
		resultChan := make(chan *runtime.Result, 1)
		resultChan <- &runtime.Result{
			Payload: payload.New("test result"),
		}
		close(resultChan)
		return resultChan, nil
	}

	// Test handler registration
	bus.Send(ctx, events.Event{
		System: runtime.System,
		Kind:   runtime.RegisterFunctionEvent,
		Path:   events.Path(target),
		Data:   handler,
	})

	time.Sleep(1 * time.Millisecond) // let event to propagate

	// Verify handler was registered
	_, exists := executor.handlers.Load(target)
	assert.True(t, exists)

	// Test handler removal
	bus.Send(ctx, events.Event{
		System: runtime.System,
		Kind:   runtime.DeleteFunctionEvent,
		Path:   events.Path(target),
	})

	time.Sleep(1 * time.Millisecond) // let event to propagate

	// Verify handler was removed
	_, exists = executor.handlers.Load(target)
	assert.False(t, exists)
}

func TestExecutor_Execute(t *testing.T) {
	ctx := context.Background()
	executor, bus := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	tests := []struct {
		name          string
		setupHandler  func(bus events.Bus)
		task          runtime.Task
		expectedErr   string
		expectedValue string
	}{
		{
			name: "successful execution",
			setupHandler: func(bus events.Bus) {
				handler := func(_ runtime.Task) (chan *runtime.Result, error) {
					resultChan := make(chan *runtime.Result, 1)
					resultChan <- &runtime.Result{
						Payload: payload.New("success"),
					}
					close(resultChan)
					return resultChan, nil
				}
				bus.Send(ctx, events.Event{
					System: runtime.System,
					Kind:   runtime.RegisterFunctionEvent,
					Path:   events.Path("test.handler"),
					Data:   handler,
				})
				time.Sleep(1 * time.Millisecond)
			},
			task: runtime.Task{
				Target:   "test.handler",
				Payloads: []payload.Payload{payload.New("test input")},
			},
			expectedValue: "success",
		},
		{
			name: "handler not found",
			task: runtime.Task{
				Target:   "nonexistent.handler",
				Payloads: []payload.Payload{payload.New("test input")},
			},
			expectedErr: "no handler registered for target: nonexistent.handler",
		},
		{
			name: "handler returns error",
			setupHandler: func(bus events.Bus) {
				handler := func(_ runtime.Task) (chan *runtime.Result, error) {
					return nil, fmt.Errorf("handler error")
				}
				bus.Send(ctx, events.Event{
					System: runtime.System,
					Kind:   runtime.RegisterFunctionEvent,
					Path:   events.Path("error.handler"),
					Data:   handler,
				})
				time.Sleep(1 * time.Millisecond)
			},
			task: runtime.Task{
				Target:   "error.handler",
				Payloads: []payload.Payload{payload.New("test input")},
			},
			expectedErr: "handler error",
		},
		{
			name: "invalid handler type",
			setupHandler: func(bus events.Bus) {
				bus.Send(ctx, events.Event{
					System: runtime.System,
					Kind:   runtime.RegisterFunctionEvent,
					Path:   events.Path("invalid.handler"),
				})
				time.Sleep(1 * time.Millisecond)
			},
			task: runtime.Task{
				Target:   "invalid.handler",
				Payloads: []payload.Payload{payload.New("test input")},
			},
			expectedErr: "no handler registered for target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupHandler != nil {
				tt.setupHandler(bus)
			}

			resultChan, err := executor.Execute(tt.task)

			if tt.expectedErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resultChan)

			result := <-resultChan
			require.NotNil(t, result)
		})
	}
}

func TestExecutor_ConcurrentHandlerRegistration(t *testing.T) {
	ctx := context.Background()
	executor, bus := setupTest()
	require.NoError(t, executor.Start(ctx))
	defer func() {
		require.NoError(t, executor.Stop())
	}()

	const numHandlers = 10
	var wg sync.WaitGroup
	wg.Add(numHandlers)

	// Register handlers concurrently
	for i := 0; i < numHandlers; i++ {
		go func(idx int) {
			defer wg.Done()
			target := registry.ID(fmt.Sprintf("test.handler.%d", idx))
			handler := func(_ runtime.Task) (chan *runtime.Result, error) {
				resultChan := make(chan *runtime.Result, 1)
				resultChan <- &runtime.Result{
					Payload: payload.New(fmt.Sprintf("result %d", idx)),
				}
				close(resultChan)
				return resultChan, nil
			}

			bus.Send(ctx, events.Event{
				System: runtime.System,
				Kind:   runtime.RegisterFunctionEvent,
				Path:   events.Path(target),
				Data:   handler,
			})
		}(i)
	}

	wg.Wait()
	time.Sleep(10 * time.Millisecond) // Allow events to propagate

	// Verify all handlers were registered
	var count int
	executor.handlers.Range(func(_, _ any) bool {
		count++
		return true
	})
	assert.Equal(t, numHandlers, count)

	// Test executing all handlers
	for i := 0; i < numHandlers; i++ {
		target := registry.ID(fmt.Sprintf("test.handler.%d", i))
		resultChan, err := executor.Execute(runtime.Task{
			Target:   target,
			Payloads: []payload.Payload{payload.New("test")},
		})
		require.NoError(t, err)
		<-resultChan
	}
}

func TestExecutor_InvalidEvents(t *testing.T) {
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
				System: runtime.System,
				Kind:   runtime.RegisterFunctionEvent,
				Data:   "invalid data",
			},
		},
		{
			name: "invalid delete handler data",
			evt: events.Event{
				System: runtime.System,
				Kind:   runtime.DeleteFunctionEvent,
				Data:   "invalid data",
			},
		},
		{
			name: "unknown event kind",
			evt: events.Event{
				System: runtime.System,
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
