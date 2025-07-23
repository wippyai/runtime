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

func TestEventBus_GetEventually(t *testing.T) {
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

	// Test GetEventually with a variable that doesn't exist yet
	// This should start waiting for the variable to be registered
	resultCh := make(chan string, 1)
	errorCh := make(chan error, 1)

	go func() {
		value, err := reg.GetEventually(ctx, "test_var")
		if err != nil {
			errorCh <- err
			return
		}
		resultCh <- value
	}()

	// Wait a bit to ensure GetEventually is waiting
	time.Sleep(50 * time.Millisecond)

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

	// Register the variable that GetEventually is waiting for
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

	// Wait for GetEventually to return the value
	select {
	case value := <-resultCh:
		assert.Equal(t, "test_value", value)
	case err := <-errorCh:
		t.Fatalf("GetEventually failed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for GetEventually to return")
	}
}

func TestEventBus_GetEventuallyWithDefaultValue(t *testing.T) {
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

	// Test GetEventually with a variable that has a default value but no actual value
	resultCh := make(chan string, 1)
	errorCh := make(chan error, 1)

	go func() {
		value, err := reg.GetEventually(ctx, "test_var_default")
		if err != nil {
			errorCh <- err
			return
		}
		resultCh <- value
	}()

	// Wait a bit to ensure GetEventually is waiting
	time.Sleep(50 * time.Millisecond)

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

	// Register the variable with a default value
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

	// Wait for GetEventually to return the default value
	select {
	case value := <-resultCh:
		assert.Equal(t, "default_value", value)
	case err := <-errorCh:
		t.Fatalf("GetEventually failed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for GetEventually to return")
	}
}

func TestEventBus_GetEventuallyContextCancellation(t *testing.T) {
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

	// Test GetEventually with context cancellation
	resultCh := make(chan string, 1)
	errorCh := make(chan error, 1)

	go func() {
		value, err := reg.GetEventually(cancelCtx, "test_var_cancel")
		if err != nil {
			errorCh <- err
			return
		}
		resultCh <- value
	}()

	// Wait a bit to ensure GetEventually is waiting
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	// Wait for GetEventually to return with context cancellation error
	select {
	case value := <-resultCh:
		t.Fatalf("GetEventually should have failed with context cancellation, but returned: %s", value)
	case err := <-errorCh:
		assert.Contains(t, err.Error(), "context canceled")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for GetEventually to return")
	}
}
