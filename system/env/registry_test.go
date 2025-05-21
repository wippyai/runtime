package env

import (
	"context"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	serviceenv "github.com/ponyruntime/pony/service/env"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestEventBus_RegisterStorageWithVariable(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	//nolint:errcheck // ok for tests
	defer logger.Sync()

	// Create a context with PID first
	ctx := context.Background()
	pid := registry.ParseID("test:ns")
	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})

	bus := eventbus.NewBus()
	defer bus.Stop()

	reg := NewRegistry(bus, logger)
	err = reg.Start(ctx)
	require.NoError(t, err)
	//nolint:errcheck // ok for tests
	defer reg.Stop()

	// Create a memory storage
	memStorage := serviceenv.NewMemoryStorage(map[string]string{
		"TEST_VAR": "test_value",
	}, logger)

	// Register storage
	evt := event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   "test:mock-storage",
		Data:   memStorage,
	}
	bus.Send(ctx, evt)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify storage was registered
	storages, err := reg.All(ctx)
	require.NoError(t, err)
	assert.Len(t, storages, 1)

	// Register a variable
	variable := env.Variable{
		Name:         "test_var",
		EnvName:      "TEST_VAR",
		StorageID:    "test:mock-storage",
		DefaultValue: "default_value",
		ReadOnly:     false,
	}
	varEvt := event.Event{
		System: env.System,
		Kind:   env.VariableRegister,
		Path:   "test:test_var",
		Data:   variable,
	}
	bus.Send(ctx, varEvt)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify we can get the value from the storage
	value, err := reg.Get(ctx, "TEST_VAR")
	require.NoError(t, err)
	assert.Equal(t, "test_value", value)
}

func TestEventBus_VariableUpdate(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	//nolint:errcheck // ok for tests
	defer logger.Sync()

	ctx := context.Background()
	bus := eventbus.NewBus()
	defer bus.Stop()

	reg := NewRegistry(bus, logger)
	err = reg.Start(ctx)
	require.NoError(t, err)
	//nolint:errcheck // ok for tests
	defer reg.Stop()

	{
		// Create a memory storage
		memStorage := serviceenv.NewMemoryStorage(map[string]string{
			"TEST_VAR": "initial_value",
		}, logger)

		// Register storage
		storageEvt := event.Event{
			System: env.System,
			Kind:   env.StorageRegister,
			Path:   "test:mock-storage",
			Data:   memStorage,
		}
		bus.Send(ctx, storageEvt)
		time.Sleep(100 * time.Millisecond)
	}

	// Register a variable
	variable := env.Variable{
		Name:         "test_var",
		EnvName:      "TEST_VAR",
		StorageID:    "test:mock-storage",
		DefaultValue: "default_value",
		ReadOnly:     false,
	}
	varEvt := event.Event{
		System: env.System,
		Kind:   env.VariableRegister,
		Path:   "test:test_var",
		Data:   variable,
	}
	bus.Send(ctx, varEvt)
	time.Sleep(100 * time.Millisecond)

	// Create a context with PID
	pid := registry.ParseID("test:ns")
	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})

	{
		// Create second a memory storage
		memStorage := serviceenv.NewMemoryStorage(map[string]string{
			"TEST_VAR": "different_value",
		}, logger)

		// Register storage
		storageEvt := event.Event{
			System: env.System,
			Kind:   env.StorageRegister,
			Path:   "test:mock-storage2",
			Data:   memStorage,
		}
		bus.Send(ctx, storageEvt)
		time.Sleep(100 * time.Millisecond)
	}

	// Update the variable
	updateEvt := event.Event{
		System: env.System,
		Kind:   env.VariableUpdate,
		Path:   "test:test_var",
		Data: env.Variable{
			Meta:         variable.Meta,
			Name:         variable.Name,
			EnvName:      variable.EnvName,
			DefaultValue: variable.DefaultValue,
			ReadOnly:     variable.ReadOnly,
			StorageID:    "test:mock-storage2",
		},
	}
	bus.Send(ctx, updateEvt)
	time.Sleep(100 * time.Millisecond)

	// Verify variable was updated
	value, err := reg.Get(ctx, "test_var")
	require.NoError(t, err)
	assert.Equal(t, "different_value", value)
}

func TestEventBus_ReadOnlyVariable(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	//nolint:errcheck // ok for tests
	defer logger.Sync()

	ctx := context.Background()
	bus := eventbus.NewBus()
	defer bus.Stop()

	reg := NewRegistry(bus, logger)
	err = reg.Start(ctx)
	require.NoError(t, err)
	//nolint:errcheck // ok for tests
	defer reg.Stop()

	// Create a memory storage
	memStorage := serviceenv.NewMemoryStorage(map[string]string{
		"TEST_VAR": "initial_value",
	}, logger)

	// Register storage
	storageEvt := event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   "test:mock-storage",
		Data:   memStorage,
	}
	bus.Send(ctx, storageEvt)
	time.Sleep(100 * time.Millisecond)

	// Register a read-only variable
	variable := env.Variable{
		Name:         "test_var",
		EnvName:      "TEST_VAR",
		StorageID:    "test:mock-storage",
		DefaultValue: "default_value",
		ReadOnly:     true,
	}
	varEvt := event.Event{
		System: env.System,
		Kind:   env.VariableRegister,
		Path:   "test:test_var",
		Data:   variable,
	}
	bus.Send(ctx, varEvt)
	time.Sleep(100 * time.Millisecond)

	// Create a context with PID
	pid := registry.ParseID("test:ns")
	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})

	err = reg.Set(ctx, "test_var", "new_value")
	require.Error(t, err, "should fail to update read-only variable")
	assert.Equal(t, env.ErrVariableReadOnly, err)

	// Verify variable was not updated
	value, err := reg.Get(ctx, "test_var")
	require.NoError(t, err)
	assert.Equal(t, "initial_value", value)
}

func TestEventBus_DuplicateVariable(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	//nolint:errcheck // ok for tests
	defer logger.Sync()

	// Create a context with PID first
	ctx := context.Background()
	pid := registry.ParseID("test:ns")
	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})

	bus := eventbus.NewBus()
	defer bus.Stop()

	// Create a channel to receive reject events
	rejectCh := make(chan event.Event, 1)
	subscriber, err := eventbus.NewSubscriber(ctx, bus, env.System, registry.Reject, func(e event.Event) {
		rejectCh <- e
	})
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subscriber.ID())

	reg := NewRegistry(bus, logger)
	err = reg.Start(ctx)
	require.NoError(t, err)
	//nolint:errcheck // ok for tests
	defer reg.Stop()

	// Create a memory storage with the variable already set
	memStorage := serviceenv.NewMemoryStorage(map[string]string{
		"TEST_VAR": "test_value",
	}, logger)

	// Register storage
	storageEvt := event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   "test:mock-storage",
		Data:   memStorage,
	}
	bus.Send(ctx, storageEvt)
	time.Sleep(100 * time.Millisecond)

	// Register first variable
	variable := env.Variable{
		Name:         "test_var",
		EnvName:      "TEST_VAR",
		StorageID:    "test:mock-storage",
		DefaultValue: "default_value",
		ReadOnly:     false,
	}
	varEvt := event.Event{
		System: env.System,
		Kind:   env.VariableRegister,
		Path:   "test:test_var",
		Data:   variable,
	}
	bus.Send(ctx, varEvt)
	time.Sleep(100 * time.Millisecond)

	// Verify first registration succeeded
	value, err := reg.Get(ctx, "test_var")
	require.NoError(t, err)
	assert.Equal(t, "test_value", value)

	// Try to register the same variable again
	duplicateVarEvt := event.Event{
		System: env.System,
		Kind:   env.VariableRegister,
		Path:   "test:test_var",
		Data:   variable,
	}
	bus.Send(ctx, duplicateVarEvt)

	// Wait for reject event with timeout
	select {
	case rejectEvt := <-rejectCh:
		assert.Equal(t, registry.Reject, rejectEvt.Kind)
		assert.Equal(t, "test:test_var", rejectEvt.Path)
		assert.Contains(t, rejectEvt.Data.(string), "variable with the name")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for reject event")
	}

	// Verify we can still get the value and it hasn't changed
	value, err = reg.Get(ctx, "test_var")
	require.NoError(t, err)
	assert.Equal(t, "test_value", value)

	// Try another duplicate registration
	duplicateVarEvt2 := event.Event{
		System: env.System,
		Kind:   env.VariableRegister,
		Path:   "test:test_var",
		Data: env.Variable{
			Name:         "test_var",
			EnvName:      "TEST_VAR",
			StorageID:    "test:mock-storage",
			DefaultValue: "new_default",
			ReadOnly:     false,
		},
	}
	bus.Send(ctx, duplicateVarEvt2)

	// Wait for reject event with timeout
	select {
	case rejectEvt := <-rejectCh:
		assert.Equal(t, registry.Reject, rejectEvt.Kind)
		assert.Equal(t, "test:test_var", rejectEvt.Path)
		assert.Contains(t, rejectEvt.Data.(string), "variable with the name")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for reject event")
	}

	// Verify the value still hasn't changed
	value, err = reg.Get(ctx, "test_var")
	require.NoError(t, err)
	assert.Equal(t, "test_value", value)
}
