package process

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupRegistryTest() (*Registry, events.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	reg := NewRegistry(bus, logger)
	return reg, bus
}

func TestRegistry_StartStop(t *testing.T) {
	ctx := context.Background()
	reg, _ := setupRegistryTest()

	// Test Start
	err := reg.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, reg.subscriber)

	// Test stop
	err = reg.Stop()
	require.NoError(t, err)
}

func TestRegistry_HandlerRegistrationOverBus(t *testing.T) {
	ctx := context.Background()
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(ctx))
	defer func() {
		require.NoError(t, reg.Stop())
	}()

	target := registry.Name("test.workflow")

	// Create a test handler
	handler := func() any {
		return "test result"
	}

	// Test handler registration
	bus.Send(ctx, events.Event{
		System: runtime.ProcessSystem,
		Kind:   runtime.RegisterSpawnCommand,
		Data: runtime.RegisterSpawn{
			ID:    target,
			Spawn: handler,
		},
	})

	time.Sleep(1 * time.Millisecond) // let event propagate

	// Verify handler was registered
	result, err := reg.Get(target)
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Test handler removal
	bus.Send(ctx, events.Event{
		System: runtime.ProcessSystem,
		Kind:   runtime.DeleteSpawnCommand,
		Data: runtime.DeleteSpawn{
			Target: target,
		},
	})

	time.Sleep(1 * time.Millisecond) // let event propagate

	// Verify handler was removed
	result, err = reg.Get(target)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestRegistry_Get(t *testing.T) {
	ctx := context.Background()
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(ctx))
	defer func() {
		require.NoError(t, reg.Stop())
	}()

	tests := []struct {
		name          string
		setupHandler  func(bus events.Bus)
		target        string
		expectedErr   string
		validateValue func(t *testing.T, value any)
	}{
		{
			name: "successful retrieval",
			setupHandler: func(bus events.Bus) {
				handler := func() any { return "success" }
				bus.Send(ctx, events.Event{
					System: runtime.ProcessSystem,
					Kind:   runtime.RegisterSpawnCommand,
					Data: runtime.RegisterSpawn{
						ID:    "test.workflow",
						Spawn: handler,
					},
				})
				time.Sleep(1 * time.Millisecond)
			},
			target: "test.workflow",
			validateValue: func(t *testing.T, value any) {
				handler, ok := value.(func() any)
				require.True(t, ok)
				assert.Equal(t, "success", handler())
			},
		},
		{
			name:        "handler not found",
			target:      "nonexistent.workflow",
			expectedErr: "no workflow handler registered for target: nonexistent.workflow",
		},
		{
			name: "nil handler registration",
			setupHandler: func(bus events.Bus) {
				bus.Send(ctx, events.Event{
					System: runtime.ProcessSystem,
					Kind:   runtime.RegisterSpawnCommand,
					Data: runtime.RegisterSpawn{
						ID:    "nil.workflow",
						Spawn: nil,
					},
				})
				time.Sleep(1 * time.Millisecond)
			},
			target:      "nil.workflow",
			expectedErr: "no workflow handler registered for target: nil.workflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupHandler != nil {
				tt.setupHandler(bus)
			}

			result, err := reg.Get(registry.Name(tt.target))

			if tt.expectedErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			if tt.validateValue != nil {
				tt.validateValue(t, result)
			}
		})
	}
}

func TestRegistry_ConcurrentHandlerRegistration(t *testing.T) {
	ctx := context.Background()
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(ctx))
	defer func() {
		require.NoError(t, reg.Stop())
	}()

	const numHandlers = 10
	var wg sync.WaitGroup
	wg.Add(numHandlers)

	// Register handlers concurrently
	for i := 0; i < numHandlers; i++ {
		go func(idx int) {
			defer wg.Done()
			target := fmt.Sprintf("test.workflow.%d", idx)
			handler := func() any {
				return fmt.Sprintf("result %d", idx)
			}

			bus.Send(ctx, events.Event{
				System: runtime.ProcessSystem,
				Kind:   runtime.RegisterSpawnCommand,
				Data: runtime.RegisterSpawn{
					ID:    registry.Name(target),
					Spawn: handler,
				},
			})
		}(i)
	}

	wg.Wait()
	time.Sleep(10 * time.Millisecond) // Allow events to propagate

	// Verify all handlers were registered
	var count int
	reg.handlers.Range(func(_, _ any) bool {
		count++
		return true
	})
	assert.Equal(t, numHandlers, count)

	// Test retrieving all handlers
	for i := 0; i < numHandlers; i++ {
		target := fmt.Sprintf("test.workflow.%d", i)
		handler, err := reg.Get(registry.Name(target))
		require.NoError(t, err)
		require.NotNil(t, handler)
		assert.Equal(t, fmt.Sprintf("result %d", i), handler())
	}
}

func TestRegistry_InvalidEvents(t *testing.T) {
	ctx := context.Background()
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(ctx))
	defer func() {
		require.NoError(t, reg.Stop())
	}()

	tests := []struct {
		name string
		evt  events.Event
	}{
		{
			name: "invalid register workflow data",
			evt: events.Event{
				System: runtime.ProcessSystem,
				Kind:   runtime.RegisterSpawnCommand,
				Data:   "invalid data",
			},
		},
		{
			name: "invalid delete workflow data",
			evt: events.Event{
				System: runtime.ProcessSystem,
				Kind:   runtime.DeleteSpawnCommand,
				Data:   "invalid data",
			},
		},
		{
			name: "unknown event kind",
			evt: events.Event{
				System: runtime.ProcessSystem,
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
