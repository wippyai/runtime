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

//func TestEventBus_MultipleSubscribers(t *testing.T) {
//	ctx := context.Background()
//	bus := eventbus.NewBus()
//
//	var registryEvents []event.Event
//	var registryEventsMutex sync.RWMutex
//	var testEvents []event.Event
//
//	// Subscriber simulating the registry's event handler
//	regSub, err := eventbus.NewSubscriber(ctx, bus, "env", "env.*", func(evt event.Event) {
//		registryEventsMutex.Lock()
//		registryEvents = append(registryEvents, evt)
//		registryEventsMutex.Unlock()
//	})
//	require.NoError(t, err)
//	defer regSub.Close()
//
//	// Subscriber simulating the test's response handler
//	testSub, err := eventbus.NewSubscriber(ctx, bus, "env", "env.*", func(evt event.Event) {
//		registryEventsMutex.Lock()
//		testEvents = append(testEvents, evt)
//		registryEventsMutex.Unlock()
//	})
//	require.NoError(t, err)
//	defer testSub.Close()
//
//	// Send an event
//	evt := event.Event{
//		System: "env",
//		Kind:   "env.StorageRegister",
//		Path:   "test:mock-storage",
//		Data:   "payload",
//	}
//	bus.Send(ctx, evt)
//
//	// Wait a bit for goroutines to process
//	time.Sleep(100 * time.Millisecond)
//
//	registryEventsMutex.Lock()
//	defer registryEventsMutex.Unlock()
//
//	assert.Len(t, registryEvents, 1, "Registry handler should receive the event")
//	assert.Len(t, testEvents, 1, "Test handler should receive the event")
//	assert.Equal(t, evt, registryEvents[0])
//	assert.Equal(t, evt, testEvents[0])
//}

func TestEventBus_RegisterStorageWithVariable(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	defer logger.Sync()

	ctx := context.Background()
	bus := eventbus.NewBus()
	defer bus.Stop()

	reg := NewRegistry(bus, logger)
	err = reg.Start(ctx)
	require.NoError(t, err)
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
		Path:   "test:var",
		Data:   variable,
	}
	bus.Send(ctx, varEvt)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Create a context with PID
	pid := registry.ParseID("test:ns")
	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})

	// Verify we can get the value from the storage
	value, err := reg.Get(ctx, "test_var")
	require.NoError(t, err)
	assert.Equal(t, "test_value", value)
}

//
//func TestEventBus_StorageDelete(t *testing.T) {
//	ctx := context.Background()
//	bus := eventbus.NewBus()
//	registry := NewRegistry(bus, zap.NewNop())
//	err := registry.Start(ctx)
//	require.NoError(t, err)
//	defer registry.Stop()
//
//	// First register a storage
//	mockStorage := &mockStorage{
//		values: make(map[string]string),
//	}
//	registerEvt := event.Event{
//		System: env.System,
//		Kind:   env.StorageRegister,
//		Path:   "test:mock-storage",
//		Data:   mockStorage,
//	}
//	bus.Send(ctx, registerEvt)
//	time.Sleep(100 * time.Millisecond)
//
//	// Then delete it
//	deleteEvt := event.Event{
//		System: env.System,
//		Kind:   env.StorageDelete,
//		Path:   "test:mock-storage",
//	}
//	bus.Send(ctx, deleteEvt)
//	time.Sleep(100 * time.Millisecond)
//
//	// Verify storage was deleted
//	storages, err := registry.All(ctx)
//	require.NoError(t, err)
//	assert.Len(t, storages, 0)
//}

//func TestEventBus_VariableRegister(t *testing.T) {
//	ctx := context.Background()
//	bus := eventbus.NewBus()
//	registry := NewRegistry(bus, zap.NewNop())
//	err := registry.Start(ctx)
//	require.NoError(t, err)
//	defer registry.Stop()
//
//	// First register a storage
//	mockStorage := &mockStorage{
//		values: map[string]string{
//			"TEST_VAR": "test_value",
//		},
//	}
//	storageEvt := event.Event{
//		System: env.System,
//		Kind:   env.StorageRegister,
//		Path:   "test:mock-storage",
//		Data:   mockStorage,
//	}
//	bus.Send(ctx, storageEvt)
//	time.Sleep(100 * time.Millisecond)
//
//	// Then register a variable
//	variable := env.Variable{
//		Name:         "test:var",
//		EnvName:      "TEST_VAR",
//		StorageID:    "test:mock-storage",
//		DefaultValue: "default_value",
//		ReadOnly:     false,
//	}
//	varEvt := event.Event{
//		System: env.System,
//		Kind:   env.VariableRegister,
//		Path:   "test:var",
//		Data:   variable,
//	}
//	bus.Send(ctx, varEvt)
//	time.Sleep(100 * time.Millisecond)
//
//	// Verify variable was registered and value can be retrieved
//	value, err := registry.Get(ctx, "test:var")
//	require.NoError(t, err)
//	assert.Equal(t, "test_value", value)
//}

// ??
//func TestEventBus_VariableDelete(t *testing.T) {
//	logger, err := zap.NewDevelopment()
//	require.NoError(t, err)
//	defer logger.Sync()
//
//	ctx := context.Background()
//	bus := eventbus.NewBus()
//	defer bus.Stop()
//
//	reg := NewRegistry(bus, logger)
//	err = reg.Start(ctx)
//	require.NoError(t, err)
//	defer reg.Stop()
//
//	// Create a memory storage
//	memStorage := serviceenv.NewMemoryStorage(map[string]string{
//		"TEST_VAR": "test_value",
//	}, logger)
//
//	// Register storage
//	storageEvt := event.Event{
//		System: env.System,
//		Kind:   env.StorageRegister,
//		Path:   "test:mock-storage",
//		Data:   memStorage,
//	}
//	bus.Send(ctx, storageEvt)
//	time.Sleep(100 * time.Millisecond)
//
//	// Register a variable
//	variable := env.Variable{
//		Name:         "test_var",
//		EnvName:      "TEST_VAR",
//		StorageID:    "test:mock-storage",
//		DefaultValue: "default_value",
//		ReadOnly:     false,
//	}
//	varEvt := event.Event{
//		System: env.System,
//		Kind:   env.VariableRegister,
//		Path:   "test:var",
//		Data:   variable,
//	}
//	bus.Send(ctx, varEvt)
//	time.Sleep(100 * time.Millisecond)
//
//	// Create a context with PID
//	pid := registry.ParseID("test:ns")
//	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})
//
//	// Verify variable was registered
//	value, err := reg.Get(ctx, "test_var")
//	require.NoError(t, err)
//	assert.Equal(t, "test_value", value)
//
//	// Delete the variable
//	deleteEvt := event.Event{
//		System: env.System,
//		Kind:   env.VariableDelete,
//		Path:   "test_var",
//	}
//	bus.Send(ctx, deleteEvt)
//	time.Sleep(100 * time.Millisecond)
//
//	// Verify variable was deleted
//	_, err = reg.Get(ctx, "test_var")
//	assert.Error(t, err)
//	assert.Equal(t, env.ErrVariableNotFound, err)
//}

func TestEventBus_VariableUpdate(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	defer logger.Sync()

	ctx := context.Background()
	bus := eventbus.NewBus()
	defer bus.Stop()

	reg := NewRegistry(bus, logger)
	err = reg.Start(ctx)
	require.NoError(t, err)
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
	defer logger.Sync()

	ctx := context.Background()
	bus := eventbus.NewBus()
	defer bus.Stop()

	reg := NewRegistry(bus, logger)
	err = reg.Start(ctx)
	require.NoError(t, err)
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
