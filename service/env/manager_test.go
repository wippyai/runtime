package env

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupManagerTest(t *testing.T) (*Manager, event.Bus) {
	t.Helper()
	bus := eventbus.NewBus()
	logger := zap.NewNop()
	manager := NewManager(bus, logger)

	t.Cleanup(func() {
		_ = manager.Stop()
		bus.Stop()
	})

	return manager, bus
}

func TestManager_StartStop(t *testing.T) {
	manager, _ := setupManagerTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := manager.Start(ctx)
	require.NoError(t, err, "Manager should start without error")

	err = manager.Stop()
	require.NoError(t, err, "Manager should stop without error")
}

func TestManager_SetGetDelete(t *testing.T) {
	manager, bus := setupManagerTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, manager.Start(ctx))

	// Test direct API calls
	manager.SetVar("TEST_VAR", "test_value")
	value, exists := manager.GetVar("TEST_VAR")
	require.True(t, exists)
	assert.Equal(t, "test_value", value)

	manager.DeleteVar("TEST_VAR")
	_, exists = manager.GetVar("TEST_VAR")
	assert.False(t, exists)

	// Test event-based operations
	responses := make(chan event.Event, 3)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		System,
		VarState,
		func(e event.Event) {
			responses <- e
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	// Test SetVar event
	bus.Send(ctx, event.Event{
		System: System,
		Kind:   SetVar,
		Path:   "EVENT_VAR",
		Data:   "event_value",
	})

	select {
	case resp := <-responses:
		assert.Equal(t, VarState, resp.Kind)
		assert.Equal(t, "EVENT_VAR", resp.Path)
		assert.Equal(t, "event_value", resp.Data)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for set response")
	}

	// Test GetVar event
	bus.Send(ctx, event.Event{
		System: System,
		Kind:   GetVar,
		Path:   "EVENT_VAR",
	})

	select {
	case resp := <-responses:
		assert.Equal(t, VarState, resp.Kind)
		assert.Equal(t, "EVENT_VAR", resp.Path)
		assert.Equal(t, "event_value", resp.Data)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for get response")
	}

	// Test DeleteVar event
	bus.Send(ctx, event.Event{
		System: System,
		Kind:   DeleteVar,
		Path:   "EVENT_VAR",
	})

	select {
	case resp := <-responses:
		assert.Equal(t, VarState, resp.Kind)
		assert.Equal(t, "EVENT_VAR", resp.Path)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for delete response")
	}

	// Verify variable is deleted
	_, exists = manager.GetVar("EVENT_VAR")
	assert.False(t, exists)
}

func TestManager_InvalidEvents(t *testing.T) {
	manager, bus := setupManagerTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, manager.Start(ctx))

	tests := []struct {
		name      string
		sendEvent event.Event
	}{
		{
			name: "invalid data type",
			sendEvent: event.Event{
				System: System,
				Kind:   SetVar,
				Path:   "TEST_VAR",
				Data:   123, // Should be string
			},
		},
		{
			name: "nil data",
			sendEvent: event.Event{
				System: System,
				Kind:   SetVar,
				Path:   "TEST_VAR",
			},
		},
		{
			name: "wrong system",
			sendEvent: event.Event{
				System: "wrong.system",
				Kind:   SetVar,
				Path:   "TEST_VAR",
				Data:   "test_value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus.Send(ctx, tt.sendEvent)
			time.Sleep(50 * time.Millisecond) // Give manager time to process

			// Variable should not be set
			_, exists := manager.GetVar(tt.sendEvent.Path)
			assert.False(t, exists)
		})
	}
}

func TestManager_ConcurrentAccess(t *testing.T) {
	manager, _ := setupManagerTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, manager.Start(ctx))

	// Test concurrent access to the same variable
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(i int) {
			manager.SetVar("CONCURRENT_VAR", fmt.Sprintf("value_%d", i))
			value, exists := manager.GetVar("CONCURRENT_VAR")
			assert.True(t, exists)
			assert.NotEmpty(t, value)
			done <- struct{}{}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for concurrent operations")
		}
	}
}
