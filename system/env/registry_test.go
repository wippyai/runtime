package env

import (
	"context"
	"fmt"
	"os"
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
	t.Parallel()
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

	// Verify storage was registered and contains variables
	variables, err := reg.All(ctx)
	require.NoError(t, err)
	assert.Contains(t, variables, "TEST_VAR")
	assert.Equal(t, "test_value", variables["TEST_VAR"])

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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

	// Try to register the same variable from a different storage
	// First register the second storage
	memStorage2 := serviceenv.NewMemoryStorage(map[string]string{
		"TEST_VAR": "different_value",
	}, logger)

	storageEvt2 := event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   "test:mock-storage2",
		Data:   memStorage2,
	}
	bus.Send(ctx, storageEvt2)
	time.Sleep(100 * time.Millisecond)

	variable2 := env.Variable{
		Name:         "test_var",
		EnvName:      "TEST_VAR",
		StorageID:    "test:mock-storage2",
		DefaultValue: "default_value",
		ReadOnly:     false,
	}
	duplicateVarEvt := event.Event{
		System: env.System,
		Kind:   env.VariableRegister,
		Path:   "test:test_var",
		Data:   variable2,
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
}

func TestEventBus_InvalidPayloads(t *testing.T) {
	t.Parallel()
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

	// Create a channel to receive reject events
	rejectCh := make(chan event.Event, 1)
	subscriber, err := eventbus.NewSubscriber(ctx, bus, env.System, registry.Reject, func(e event.Event) {
		rejectCh <- e
	})
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subscriber.ID())

	// Test invalid storage payload
	invalidStorageEvt := event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   "test:mock-storage",
		Data:   "invalid-storage",
	}
	bus.Send(ctx, invalidStorageEvt)

	select {
	case rejectEvt := <-rejectCh:
		assert.Equal(t, registry.Reject, rejectEvt.Kind)
		assert.Equal(t, "test:mock-storage", rejectEvt.Path)
		assert.Contains(t, rejectEvt.Data.(string), "invalid storage data type")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for reject event")
	}

	// Test invalid variable payload
	invalidVarEvt := event.Event{
		System: env.System,
		Kind:   env.VariableRegister,
		Path:   "test:test_var",
		Data:   "invalid-variable",
	}
	bus.Send(ctx, invalidVarEvt)

	select {
	case rejectEvt := <-rejectCh:
		assert.Equal(t, registry.Reject, rejectEvt.Kind)
		assert.Equal(t, "test:test_var", rejectEvt.Path)
		assert.Contains(t, rejectEvt.Data.(string), "invalid variable data type")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for reject event")
	}
}

func TestEventBus_NamespaceHandling(t *testing.T) {
	t.Parallel()
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

	// Register variable in test namespace
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

	// Test getting variable with explicit namespace
	pid := registry.ParseID("test:ns")
	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})
	value, err := reg.Get(ctx, "test:test_var")
	require.NoError(t, err)
	assert.Equal(t, "test_value", value)

	// Test getting variable without namespace (should use default)
	value, err = reg.Get(ctx, "test_var")
	require.NoError(t, err)
	assert.Equal(t, "test_value", value)
}

func TestEventBus_AllStorages(t *testing.T) {
	t.Parallel()
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

	// Create two memory storages
	memStorage1 := serviceenv.NewMemoryStorage(map[string]string{
		"TEST_VAR1": "value1",
	}, logger)
	memStorage2 := serviceenv.NewMemoryStorage(map[string]string{
		"TEST_VAR2": "value2",
	}, logger)

	// Register storages
	storageEvt1 := event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   "test:mock-storage1",
		Data:   memStorage1,
	}
	storageEvt2 := event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   "test:mock-storage2",
		Data:   memStorage2,
	}
	bus.Send(ctx, storageEvt1)
	bus.Send(ctx, storageEvt2)
	time.Sleep(100 * time.Millisecond)

	// Get all variables from all storages
	variables, err := reg.All(ctx)
	require.NoError(t, err)
	assert.Contains(t, variables, "TEST_VAR1")
	assert.Equal(t, "value1", variables["TEST_VAR1"])
	assert.Contains(t, variables, "TEST_VAR2")
	assert.Equal(t, "value2", variables["TEST_VAR2"])
}

func TestEventBus_NotFoundCases(t *testing.T) {
	t.Parallel()
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

	// Test getting non-existent variable
	_, err = reg.Get(ctx, "non_existent_var")
	require.Error(t, err)
	assert.Equal(t, env.ErrVariableNotFound, err)

	// Test setting non-existent variable
	err = reg.Set(ctx, "non_existent_var", "value")
	require.Error(t, err)
	assert.Equal(t, env.ErrVariableNotFound, err)

	// Create a memory storage
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

	// Register variable with non-existent storage
	variable := env.Variable{
		Name:         "test_var",
		EnvName:      "TEST_VAR",
		StorageID:    "test:non-existent-storage",
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

	// Verify variable was not registered
	_, err = reg.Get(ctx, "test_var")
	require.Error(t, err)
	assert.Equal(t, env.ErrVariableNotFound, err)
}

func TestEventBus_GetFromStorage(t *testing.T) {
	t.Parallel()
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

	// Create a context with PID
	pid := registry.ParseID("test:ns")
	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})

	// Create a memory storage first
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

	// Now test GetFromStorage with the registered storage
	value, err := reg.GetFromStorage(ctx, "test:mock-storage:TEST_VAR")
	require.NoError(t, err)
	assert.Equal(t, "test_value", value)
}

func TestEventBus_GetFromStorageWithDefaultValue(t *testing.T) {
	t.Parallel()
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

	// Create a context with PID
	pid := registry.ParseID("test:ns")
	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})

	// Create a memory storage with empty value
	memStorage := serviceenv.NewMemoryStorage(map[string]string{
		"TEST_VAR_DEFAULT": "", // Empty value, should use default
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

	// Now test GetFromStorage with empty value (should return empty string)
	value, err := reg.GetFromStorage(ctx, "test:mock-storage:TEST_VAR_DEFAULT")
	require.NoError(t, err)
	assert.Equal(t, "", value)
}

func TestEventBus_GetFromStorageContextCancellation(t *testing.T) {
	t.Parallel()
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

	// Create a context with PID
	pid := registry.ParseID("test:ns")
	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})

	// Create a cancellable context
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Test GetFromStorage with non-existent storage (should fail immediately)
	_, err = reg.GetFromStorage(cancelCtx, "test:non-existent-storage:TEST_VAR_CANCEL")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "environment variable not found")
}

func TestEventBus_ThreeAccessModes(t *testing.T) {
	// Remove t.Parallel() to avoid interference between tests
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck // ok for tests
		logger.Sync()
	})

	ctx := context.Background()
	bus := eventbus.NewBus()
	t.Cleanup(func() {
		bus.Stop()
	})

	reg := NewRegistry(bus, logger)
	err = reg.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck // ok for tests
		reg.Stop()
	})

	// Create a memory storage
	memStorage := serviceenv.NewMemoryStorage(map[string]string{
		"FILE_TEST_ENV":          "file_value",
		"FILE_TEST_ENV_READONLY": "file_value_readonly",
	}, logger)

	// Register storage
	storageEvt := event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   "app.env.demo:envfile",
		Data:   memStorage,
	}
	bus.Send(ctx, storageEvt)
	// time.Sleep(100 * time.Millisecond)

	// Register variables
	variables := []env.Variable{
		{
			Name:         "file_test_env",
			EnvName:      "FILE_TEST_ENV",
			StorageID:    "app.env.demo:envfile",
			DefaultValue: "default_value",
			ReadOnly:     false,
		},
		{
			Name:         "file_test_env_readonly",
			EnvName:      "FILE_TEST_ENV_READONLY",
			StorageID:    "app.env.demo:envfile",
			DefaultValue: "default_value",
			ReadOnly:     true,
		},
	}

	for _, variable := range variables {
		varEvt := event.Event{
			System: env.System,
			Kind:   env.VariableRegister,
			Path:   "app.env.demo:" + variable.Name,
			Data:   variable,
		}
		bus.Send(ctx, varEvt)
	}
	time.Sleep(10 * time.Millisecond)

	// Create a context with PID
	pid := registry.ParseID("app.env.demo:test")
	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})

	// Test all three access modes
	t.Run("AccessByName", func(t *testing.T) {
		// Remove t.Parallel() to avoid interference
		value, err := reg.Get(ctx, "file_test_env")
		require.NoError(t, err)
		assert.Equal(t, "file_value", value)
	})

	t.Run("AccessByFullName", func(t *testing.T) {
		// Remove t.Parallel() to avoid interference
		value, err := reg.Get(ctx, "app.env.demo:file_test_env")
		require.NoError(t, err)
		assert.Equal(t, "file_value", value)
	})

	t.Run("AccessByEnvName", func(t *testing.T) {
		// Remove t.Parallel() to avoid interference
		value, err := reg.Get(ctx, "FILE_TEST_ENV")
		require.NoError(t, err)
		assert.Equal(t, "file_value", value)
	})

	t.Run("CrossVerification", func(t *testing.T) {
		// Remove t.Parallel() to avoid interference
		// Set a value using one mode
		err := reg.Set(ctx, "file_test_env", "cross_verification_value")
		require.NoError(t, err)

		// Verify all three modes return the same value
		value1, err := reg.Get(ctx, "file_test_env")
		require.NoError(t, err)

		value2, err := reg.Get(ctx, "app.env.demo:file_test_env")
		require.NoError(t, err)

		value3, err := reg.Get(ctx, "FILE_TEST_ENV")
		require.NoError(t, err)

		assert.Equal(t, "cross_verification_value", value1)
		assert.Equal(t, "cross_verification_value", value2)
		assert.Equal(t, "cross_verification_value", value3)
	})
}

func TestEventBus_ThreeAccessModesWithDifferentNamespaces(t *testing.T) {
	// Remove t.Parallel() to avoid interference between tests
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck // ok for tests
		logger.Sync()
	})

	ctx := context.Background()
	bus := eventbus.NewBus()
	t.Cleanup(func() {
		bus.Stop()
	})

	reg := NewRegistry(bus, logger)
	err = reg.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck // ok for tests
		reg.Stop()
	})

	// Create storages for different namespaces
	storage1 := serviceenv.NewMemoryStorage(map[string]string{
		"TEST_VAR_NS1": "namespace1_value",
	}, logger)
	storage2 := serviceenv.NewMemoryStorage(map[string]string{
		"TEST_VAR_NS2": "namespace2_value",
	}, logger)

	// Register storages
	storage1Evt := event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   "namespace1:storage",
		Data:   storage1,
	}
	bus.Send(ctx, storage1Evt)

	storage2Evt := event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   "namespace2:storage",
		Data:   storage2,
	}
	bus.Send(ctx, storage2Evt)
	time.Sleep(100 * time.Millisecond)

	// Register variables
	variables := []env.Variable{
		{
			Name:         "test_var",
			EnvName:      "TEST_VAR_NS1",
			StorageID:    "namespace1:storage",
			DefaultValue: "default_value",
			ReadOnly:     false,
		},
		{
			Name:         "test_var",
			EnvName:      "TEST_VAR_NS2",
			StorageID:    "namespace2:storage",
			DefaultValue: "default_value",
			ReadOnly:     false,
		},
	}

	for i, variable := range variables {
		varEvt := event.Event{
			System: env.System,
			Kind:   env.VariableRegister,
			Path:   fmt.Sprintf("namespace%d:test_var", i+1), // Use different paths
			Data:   variable,
		}
		bus.Send(ctx, varEvt)
	}
	time.Sleep(100 * time.Millisecond)

	// Test namespace1 context
	t.Run("Namespace1Context", func(t *testing.T) {
		// Remove t.Parallel() to avoid interference
		pid := registry.ParseID("namespace1:test")
		ctx := pubsub.WithPID(context.Background(), pubsub.PID{ID: pid})

		// Test by name (should add namespace1)
		value, err := reg.Get(ctx, "test_var")
		require.NoError(t, err)
		assert.Equal(t, "namespace1_value", value)

		// Test by full name
		value, err = reg.Get(ctx, "namespace1:test_var")
		require.NoError(t, err)
		assert.Equal(t, "namespace1_value", value)

		// Test by ENV name
		value, err = reg.Get(ctx, "TEST_VAR_NS1")
		require.NoError(t, err)
		assert.Equal(t, "namespace1_value", value)
	})

	// Test namespace2 context
	t.Run("Namespace2Context", func(t *testing.T) {
		// Remove t.Parallel() to avoid interference
		pid := registry.ParseID("namespace2:test")
		ctx := pubsub.WithPID(context.Background(), pubsub.PID{ID: pid})

		// Test by name (should add namespace2)
		value, err := reg.Get(ctx, "test_var")
		require.NoError(t, err)
		assert.Equal(t, "namespace2_value", value)

		// Test by full name
		value, err = reg.Get(ctx, "namespace2:test_var")
		require.NoError(t, err)
		assert.Equal(t, "namespace2_value", value)

		// Test by ENV name
		value, err = reg.Get(ctx, "TEST_VAR_NS2")
		require.NoError(t, err)
		assert.Equal(t, "namespace2_value", value)
	})

	// Test explicit namespace access
	t.Run("ExplicitNamespaceAccess", func(t *testing.T) {
		// Remove t.Parallel() to avoid interference
		ctx := context.Background()

		// Test accessing namespace2 variable from any context
		value, err := reg.Get(ctx, "namespace2:test_var")
		require.NoError(t, err)
		assert.Equal(t, "namespace2_value", value)

		// Test accessing namespace1 variable from any context
		value, err = reg.Get(ctx, "namespace1:test_var")
		require.NoError(t, err)
		assert.Equal(t, "namespace1_value", value)
	})
}

func TestEventBus_ThreeAccessModesNotFound(t *testing.T) {
	t.Parallel()
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck // ok for tests
		logger.Sync()
	})

	ctx := context.Background()
	bus := eventbus.NewBus()
	t.Cleanup(func() {
		bus.Stop()
	})

	reg := NewRegistry(bus, logger)
	err = reg.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck // ok for tests
		reg.Stop()
	})

	// Create a context with PID
	pid := registry.ParseID("test:ns")
	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})

	// Test that all three modes return the same error for non-existent variables
	t.Run("NotFoundByName", func(t *testing.T) {
		t.Parallel()
		_, err := reg.Get(ctx, "non_existent_var")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("NotFoundByFullName", func(t *testing.T) {
		t.Parallel()
		_, err := reg.Get(ctx, "test:non_existent_var")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("NotFoundByEnvName", func(t *testing.T) {
		t.Parallel()
		_, err := reg.Get(ctx, "NON_EXISTENT_VAR")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestEventBus_ThreeAccessModesWithDefaultValues(t *testing.T) {
	t.Parallel()
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck // ok for tests
		logger.Sync()
	})

	ctx := context.Background()
	bus := eventbus.NewBus()
	t.Cleanup(func() {
		bus.Stop()
	})

	reg := NewRegistry(bus, logger)
	err = reg.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck // ok for tests
		reg.Stop()
	})

	// Create a memory storage with empty value
	memStorage := serviceenv.NewMemoryStorage(map[string]string{
		"TEST_VAR_DEFAULT": "", // Empty value, should use default
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

	// Register variable with default value
	variable := env.Variable{
		Name:         "test_var_default",
		EnvName:      "TEST_VAR_DEFAULT",
		StorageID:    "test:mock-storage",
		DefaultValue: "default_value",
		ReadOnly:     false,
	}
	varEvt := event.Event{
		System: env.System,
		Kind:   env.VariableRegister,
		Path:   "test:test_var_default",
		Data:   variable,
	}
	bus.Send(ctx, varEvt)
	time.Sleep(100 * time.Millisecond)

	// Create a context with PID
	pid := registry.ParseID("test:ns")
	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})

	// Test all three modes with default values
	t.Run("DefaultValueByName", func(t *testing.T) {
		t.Parallel()
		value, err := reg.Get(ctx, "test_var_default")
		require.NoError(t, err)
		assert.Equal(t, "default_value", value)
	})

	t.Run("DefaultValueByFullName", func(t *testing.T) {
		t.Parallel()
		value, err := reg.Get(ctx, "test:test_var_default")
		require.NoError(t, err)
		assert.Equal(t, "default_value", value)
	})

	t.Run("DefaultValueByEnvName", func(t *testing.T) {
		t.Parallel()
		value, err := reg.Get(ctx, "TEST_VAR_DEFAULT")
		require.NoError(t, err)
		assert.Equal(t, "default_value", value)
	})
}

func TestEventBus_ThreeAccessModesWithAllStorageTypes(t *testing.T) {
	t.Parallel()
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck // ok for tests
		logger.Sync()
	})

	ctx := context.Background()
	bus := eventbus.NewBus()
	t.Cleanup(func() {
		bus.Stop()
	})

	reg := NewRegistry(bus, logger)
	err = reg.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck // ok for tests
		reg.Stop()
	})

	// Create a context with PID
	pid := registry.ParseID("test:ns")
	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})

	// Test 1: Memory Storage
	t.Run("MemoryStorage", func(t *testing.T) {
		t.Parallel()
		// Create memory storage
		memStorage := serviceenv.NewMemoryStorage(map[string]string{
			"MEMORY_TEST_VAR": "memory_value",
		}, logger)

		// Register storage
		storageEvt := event.Event{
			System: env.System,
			Kind:   env.StorageRegister,
			Path:   "test:memory-storage",
			Data:   memStorage,
		}
		bus.Send(ctx, storageEvt)
		time.Sleep(100 * time.Millisecond)

		// Register variable
		variable := env.Variable{
			Name:         "memory_test_var",
			EnvName:      "MEMORY_TEST_VAR",
			StorageID:    "test:memory-storage",
			DefaultValue: "default_value",
			ReadOnly:     false,
		}
		varEvt := event.Event{
			System: env.System,
			Kind:   env.VariableRegister,
			Path:   "test:memory_test_var",
			Data:   variable,
		}
		bus.Send(ctx, varEvt)
		time.Sleep(100 * time.Millisecond)

		// Test all three access modes
		value, err := reg.Get(ctx, "memory_test_var")
		require.NoError(t, err)
		assert.Equal(t, "memory_value", value)

		value, err = reg.Get(ctx, "test:memory_test_var")
		require.NoError(t, err)
		assert.Equal(t, "memory_value", value)

		value, err = reg.Get(ctx, "MEMORY_TEST_VAR")
		require.NoError(t, err)
		assert.Equal(t, "memory_value", value)

		// Test setting value
		err = reg.Set(ctx, "memory_test_var", "new_memory_value")
		require.NoError(t, err)

		value, err = reg.Get(ctx, "memory_test_var")
		require.NoError(t, err)
		assert.Equal(t, "new_memory_value", value)
	})

	// Test 2: File Storage
	t.Run("FileStorage", func(t *testing.T) {
		t.Parallel()
		// Create temporary file for testing
		tempFile, err := os.CreateTemp("", "env_test_*.env")
		require.NoError(t, err)
		t.Cleanup(func() {
			os.Remove(tempFile.Name())
		})

		// Write test data to file
		_, err = tempFile.WriteString("FILE_TEST_VAR=file_value\n")
		require.NoError(t, err)
		tempFile.Close()

		// Create file storage
		fileStorage := serviceenv.NewFileStorage(tempFile.Name(), logger)

		// Register storage
		storageEvt := event.Event{
			System: env.System,
			Kind:   env.StorageRegister,
			Path:   "test:file-storage",
			Data:   fileStorage,
		}
		bus.Send(ctx, storageEvt)
		time.Sleep(100 * time.Millisecond)

		// Register variable
		variable := env.Variable{
			Name:         "file_test_var",
			EnvName:      "FILE_TEST_VAR",
			StorageID:    "test:file-storage",
			DefaultValue: "default_value",
			ReadOnly:     false,
		}
		varEvt := event.Event{
			System: env.System,
			Kind:   env.VariableRegister,
			Path:   "test:file_test_var",
			Data:   variable,
		}
		bus.Send(ctx, varEvt)
		time.Sleep(100 * time.Millisecond)

		// Test all three access modes
		value, err := reg.Get(ctx, "file_test_var")
		require.NoError(t, err)
		assert.Equal(t, "file_value", value)

		value, err = reg.Get(ctx, "test:file_test_var")
		require.NoError(t, err)
		assert.Equal(t, "file_value", value)

		value, err = reg.Get(ctx, "FILE_TEST_VAR")
		require.NoError(t, err)
		assert.Equal(t, "file_value", value)

		// Test setting value
		err = reg.Set(ctx, "file_test_var", "new_file_value")
		require.NoError(t, err)

		value, err = reg.Get(ctx, "file_test_var")
		require.NoError(t, err)
		assert.Equal(t, "new_file_value", value)
	})

	// Test 3: OS Storage
	t.Run("OSStorage", func(t *testing.T) {
		t.Parallel()
		// Set a test environment variable
		err := os.Setenv("OS_TEST_VAR", "os_value")
		require.NoError(t, err)
		t.Cleanup(func() {
			os.Unsetenv("OS_TEST_VAR")
		})

		// Create OS storage
		osStorage := serviceenv.NewOSStorage(logger)

		// Register storage
		storageEvt := event.Event{
			System: env.System,
			Kind:   env.StorageRegister,
			Path:   "test:os-storage",
			Data:   osStorage,
		}
		bus.Send(ctx, storageEvt)
		time.Sleep(100 * time.Millisecond)

		// Register variable
		variable := env.Variable{
			Name:         "os_test_var",
			EnvName:      "OS_TEST_VAR",
			StorageID:    "test:os-storage",
			DefaultValue: "default_value",
			ReadOnly:     true, // OS storage is read-only
		}
		varEvt := event.Event{
			System: env.System,
			Kind:   env.VariableRegister,
			Path:   "test:os_test_var",
			Data:   variable,
		}
		bus.Send(ctx, varEvt)
		time.Sleep(100 * time.Millisecond)

		// Test all three access modes
		value, err := reg.Get(ctx, "os_test_var")
		require.NoError(t, err)
		assert.Equal(t, "os_value", value)

		value, err = reg.Get(ctx, "test:os_test_var")
		require.NoError(t, err)
		assert.Equal(t, "os_value", value)

		value, err = reg.Get(ctx, "OS_TEST_VAR")
		require.NoError(t, err)
		assert.Equal(t, "os_value", value)

		// Test that setting fails (read-only)
		err = reg.Set(ctx, "os_test_var", "new_os_value")
		require.Error(t, err)
		assert.Equal(t, env.ErrVariableReadOnly, err)
	})

	// Test 4: Router Storage
	t.Run("RouterStorage", func(t *testing.T) {
		t.Parallel()
		// Create memory storage for router
		memStorage := serviceenv.NewMemoryStorage(map[string]string{
			"ROUTER_TEST_VAR": "memory_value",
		}, logger)

		// Create OS storage for router (will use existing OS_TEST_VAR)
		osStorage := serviceenv.NewOSStorage(logger)

		// Create router storage
		routerStorage, err := serviceenv.NewRouterStorage([]env.Storage{memStorage, osStorage}, logger)
		require.NoError(t, err)

		// Register router storage
		storageEvt := event.Event{
			System: env.System,
			Kind:   env.StorageRegister,
			Path:   "test:router-storage",
			Data:   routerStorage,
		}
		bus.Send(ctx, storageEvt)
		time.Sleep(100 * time.Millisecond)

		// Register variable
		variable := env.Variable{
			Name:         "router_test_var",
			EnvName:      "ROUTER_TEST_VAR",
			StorageID:    "test:router-storage",
			DefaultValue: "default_value",
			ReadOnly:     false,
		}
		varEvt := event.Event{
			System: env.System,
			Kind:   env.VariableRegister,
			Path:   "test:router_test_var",
			Data:   variable,
		}
		bus.Send(ctx, varEvt)
		time.Sleep(100 * time.Millisecond)

		// Test all three access modes
		value, err := reg.Get(ctx, "router_test_var")
		require.NoError(t, err)
		assert.Equal(t, "memory_value", value)

		value, err = reg.Get(ctx, "test:router_test_var")
		require.NoError(t, err)
		assert.Equal(t, "memory_value", value)

		value, err = reg.Get(ctx, "ROUTER_TEST_VAR")
		require.NoError(t, err)
		assert.Equal(t, "memory_value", value)

		// Test setting value (should go to primary storage)
		err = reg.Set(ctx, "router_test_var", "new_router_value")
		require.NoError(t, err)

		value, err = reg.Get(ctx, "router_test_var")
		require.NoError(t, err)
		assert.Equal(t, "new_router_value", value)

		// Test fallback to OS storage for a variable not in memory
		// Set a new OS environment variable
		err = os.Setenv("ROUTER_FALLBACK_VAR", "fallback_value")
		require.NoError(t, err)
		t.Cleanup(func() {
			os.Unsetenv("ROUTER_FALLBACK_VAR")
		})

		// Register fallback variable
		fallbackVariable := env.Variable{
			Name:         "router_fallback_var",
			EnvName:      "ROUTER_FALLBACK_VAR",
			StorageID:    "test:router-storage",
			DefaultValue: "default_value",
			ReadOnly:     false,
		}
		fallbackEvt := event.Event{
			System: env.System,
			Kind:   env.VariableRegister,
			Path:   "test:router_fallback_var",
			Data:   fallbackVariable,
		}
		bus.Send(ctx, fallbackEvt)
		time.Sleep(100 * time.Millisecond)

		// Test fallback (should get from OS storage)
		value, err = reg.Get(ctx, "router_fallback_var")
		require.NoError(t, err)
		assert.Equal(t, "fallback_value", value)

		value, err = reg.Get(ctx, "test:router_fallback_var")
		require.NoError(t, err)
		assert.Equal(t, "fallback_value", value)

		value, err = reg.Get(ctx, "ROUTER_FALLBACK_VAR")
		require.NoError(t, err)
		assert.Equal(t, "fallback_value", value)
	})
}

func TestEventBus_ThreeAccessModesWithRouterStorageComplex(t *testing.T) {
	// Remove t.Parallel() to avoid interference between tests
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck // ok for tests
		logger.Sync()
	})

	ctx := context.Background()
	bus := eventbus.NewBus()
	t.Cleanup(func() {
		bus.Stop()
	})

	reg := NewRegistry(bus, logger)
	err = reg.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck // ok for tests
		reg.Stop()
	})

	// Create a context with PID
	pid := registry.ParseID("test:ns")
	ctx = pubsub.WithPID(ctx, pubsub.PID{ID: pid})

	// Create multiple storages for complex router test
	memStorage := serviceenv.NewMemoryStorage(map[string]string{
		"ROUTER_MEMORY_VAR": "memory_value",
	}, logger)

	// Create temporary file for file storage
	tempFile, err := os.CreateTemp("", "env_test_*.env")
	require.NoError(t, err)
	t.Cleanup(func() {
		os.Remove(tempFile.Name())
	})

	// Write test data to file
	_, err = tempFile.WriteString("ROUTER_FILE_VAR=file_value\n")
	require.NoError(t, err)
	tempFile.Close()

	fileStorage := serviceenv.NewFileStorage(tempFile.Name(), logger)

	// Set OS environment variable
	err = os.Setenv("ROUTER_OS_VAR", "os_value")
	require.NoError(t, err)
	t.Cleanup(func() {
		os.Unsetenv("ROUTER_OS_VAR")
	})

	osStorage := serviceenv.NewOSStorage(logger)

	// Create router storage with all three storages
	routerStorage, err := serviceenv.NewRouterStorage([]env.Storage{memStorage, fileStorage, osStorage}, logger)
	require.NoError(t, err)

	// Register router storage
	storageEvt := event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   "test:complex-router-storage",
		Data:   routerStorage,
	}
	bus.Send(ctx, storageEvt)
	time.Sleep(100 * time.Millisecond)

	// Register variables for each storage type
	variables := []env.Variable{
		{
			Name:         "router_memory_var",
			EnvName:      "ROUTER_MEMORY_VAR",
			StorageID:    "test:complex-router-storage",
			DefaultValue: "default_value",
			ReadOnly:     false,
		},
		{
			Name:         "router_file_var",
			EnvName:      "ROUTER_FILE_VAR",
			StorageID:    "test:complex-router-storage",
			DefaultValue: "default_value",
			ReadOnly:     false,
		},
		{
			Name:         "router_os_var",
			EnvName:      "ROUTER_OS_VAR",
			StorageID:    "test:complex-router-storage",
			DefaultValue: "default_value",
			ReadOnly:     false,
		},
	}

	for _, variable := range variables {
		varEvt := event.Event{
			System: env.System,
			Kind:   env.VariableRegister,
			Path:   "test:" + variable.Name,
			Data:   variable,
		}
		bus.Send(ctx, varEvt)
		time.Sleep(100 * time.Millisecond)
	}

	// Test all three access modes for each variable type
	t.Run("RouterMemoryVariable", func(t *testing.T) {
		// Remove t.Parallel() to avoid interference
		// Test by name
		value, err := reg.Get(ctx, "router_memory_var")
		require.NoError(t, err)
		assert.Equal(t, "memory_value", value)

		// Test by full name
		value, err = reg.Get(ctx, "test:router_memory_var")
		require.NoError(t, err)
		assert.Equal(t, "memory_value", value)

		// Test by ENV name
		value, err = reg.Get(ctx, "ROUTER_MEMORY_VAR")
		require.NoError(t, err)
		assert.Equal(t, "memory_value", value)

		// Test setting (should go to primary storage)
		err = reg.Set(ctx, "router_memory_var", "new_memory_value")
		require.NoError(t, err)

		value, err = reg.Get(ctx, "router_memory_var")
		require.NoError(t, err)
		assert.Equal(t, "new_memory_value", value)
	})

	t.Run("RouterFileVariable", func(t *testing.T) {
		// Remove t.Parallel() to avoid interference
		// Test by name
		value, err := reg.Get(ctx, "router_file_var")
		require.NoError(t, err)
		assert.Equal(t, "file_value", value)

		// Test by full name
		value, err = reg.Get(ctx, "test:router_file_var")
		require.NoError(t, err)
		assert.Equal(t, "file_value", value)

		// Test by ENV name
		value, err = reg.Get(ctx, "ROUTER_FILE_VAR")
		require.NoError(t, err)
		assert.Equal(t, "file_value", value)

		// Test setting (should go to primary storage)
		err = reg.Set(ctx, "router_file_var", "new_file_value")
		require.NoError(t, err)

		value, err = reg.Get(ctx, "router_file_var")
		require.NoError(t, err)
		assert.Equal(t, "new_file_value", value)
	})

	t.Run("RouterOSVariable", func(t *testing.T) {
		// Remove t.Parallel() to avoid interference
		// Test by name
		value, err := reg.Get(ctx, "router_os_var")
		require.NoError(t, err)
		assert.Equal(t, "os_value", value)

		// Test by full name
		value, err = reg.Get(ctx, "test:router_os_var")
		require.NoError(t, err)
		assert.Equal(t, "os_value", value)

		// Test by ENV name
		value, err = reg.Get(ctx, "ROUTER_OS_VAR")
		require.NoError(t, err)
		assert.Equal(t, "os_value", value)

		// Test setting (should go to primary storage)
		err = reg.Set(ctx, "router_os_var", "new_os_value")
		require.NoError(t, err)

		value, err = reg.Get(ctx, "router_os_var")
		require.NoError(t, err)
		assert.Equal(t, "new_os_value", value)
	})

	// Test cross-verification that all modes return the same value
	t.Run("CrossVerification", func(t *testing.T) {
		// Remove t.Parallel() to avoid interference
		// Set a value using one mode
		err := reg.Set(ctx, "router_memory_var", "cross_verification_value")
		require.NoError(t, err)

		// Verify all three modes return the same value
		value1, err := reg.Get(ctx, "router_memory_var")
		require.NoError(t, err)

		value2, err := reg.Get(ctx, "test:router_memory_var")
		require.NoError(t, err)

		value3, err := reg.Get(ctx, "ROUTER_MEMORY_VAR")
		require.NoError(t, err)

		assert.Equal(t, "cross_verification_value", value1)
		assert.Equal(t, "cross_verification_value", value2)
		assert.Equal(t, "cross_verification_value", value3)
	})
}
