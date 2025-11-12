package env

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/ponyruntime/pony/system/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockStorage implements env.Storage for testing with thread safety
type mockStorage struct {
	data  map[string]string
	mutex sync.RWMutex
}

func newMockStorage(data map[string]string) *mockStorage {
	if data == nil {
		data = make(map[string]string)
	}
	return &mockStorage{data: data}
}

func (m *mockStorage) Get(_ context.Context, name string) (string, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if value, exists := m.data[name]; exists {
		return value, nil
	}
	return "", env.ErrVariableNotFound
}

func (m *mockStorage) Set(_ context.Context, name, value string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.data[name] = value
	return nil
}

func (m *mockStorage) Delete(_ context.Context, name string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	delete(m.data, name)
	return nil
}

func (m *mockStorage) List(_ context.Context) (map[string]string, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make(map[string]string)
	for k, v := range m.data {
		result[k] = v
	}
	return result, nil
}

func setupTestRegistry() (*Registry, event.Bus, context.Context) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	reg := NewRegistry(bus, logger)
	ctx := ctxapi.NewRootContext()
	ctx = pubsubapi.WithNode(ctx, pubsub.NewNode("test"))
	return reg, bus, ctx
}

// safeStop safely stops the registry and logs any errors
func safeStop(t *testing.T, reg *Registry) {
	if err := reg.Stop(); err != nil {
		t.Logf("failed to stop registry: %v", err)
	}
}

func TestRegistry_Get_VariableWithName(t *testing.T) {
	reg, _, ctx := setupTestRegistry()
	require.NoError(t, reg.Start(ctx))
	defer func() {
		if err := reg.Stop(); err != nil {
			t.Logf("failed to stop registry: %v", err)
		}
	}()

	// Setup storage
	storage := newMockStorage(map[string]string{
		"test_var": "test_value",
	})
	storageID := registry.ParseID("app:storage")
	reg.storages.Store(storageID, storage)

	// Register variable with Name
	variable := env.Variable{
		Name:      "test_var", // getEnvName() will return this
		StorageID: storageID,
	}
	reg.variablesByID.Store(variable.ID, variable)
	reg.variablesByName.Store("test_var", variable.ID) // Stored under Name

	// Can access by Name
	value, err := reg.Get(ctx, "test_var")
	require.NoError(t, err)
	assert.Equal(t, "test_value", value)

	// Can also access by ID string (fallback behavior)
	value, err = reg.Get(ctx, "app:my_var")
	require.NoError(t, err)
	assert.Equal(t, "test_value", value)
}

func TestRegistry_Get_VariableWithoutName(t *testing.T) {
	reg, _, ctx := setupTestRegistry()
	require.NoError(t, reg.Start(ctx))
	defer func() {
		if err := reg.Stop(); err != nil {
			t.Logf("failed to stop registry: %v", err)
		}
	}()

	// Setup storage
	storage := newMockStorage(map[string]string{
		"app:my_var": "id_value", // Storage key is full ID string
	})
	storageID := registry.ParseID("app:storage")
	reg.storages.Store(storageID, storage)

	// Register variable without Name
	variable := env.Variable{
		Name:      "", // Empty name - getEnvName() will return ID.String()
		StorageID: storageID,
	}
	reg.variablesByID.Store(variable.ID, variable)
	reg.variablesByName.Store("app:my_var", variable.ID) // Stored under full ID string

	// Can access by full ID string
	value, err := reg.Get(ctx, "app:my_var")
	require.NoError(t, err)
	assert.Equal(t, "id_value", value)

	// Cannot access by short name (not registered)
	_, err = reg.Get(ctx, "my_var")
	require.Error(t, err)
	assert.Equal(t, env.ErrVariableNotFound, err)
}

func TestRegistry_Set_VariableWithName(t *testing.T) {
	reg, _, ctx := setupTestRegistry()
	require.NoError(t, reg.Start(ctx))
	defer func() {
		if err := reg.Stop(); err != nil {
			t.Logf("failed to stop registry: %v", err)
		}
	}()

	// Setup storage
	storage := newMockStorage(map[string]string{
		"test_var": "initial_value",
	})
	storageID := registry.ParseID("app:storage")
	reg.storages.Store(storageID, storage)

	// Register variable with Name
	variable := env.Variable{
		Name:      "test_var",
		StorageID: storageID,
		ReadOnly:  false,
	}
	reg.variablesByID.Store(variable.ID, variable)
	reg.variablesByName.Store("test_var", variable.ID)

	// Can set by Name
	err := reg.Set(ctx, "test_var", "new_value")
	require.NoError(t, err)

	// Verify change
	value, err := reg.Get(ctx, "test_var")
	require.NoError(t, err)
	assert.Equal(t, "new_value", value)

	// Can also set by ID string (fallback behavior)
	err = reg.Set(ctx, "app:my_var", "id_value")
	require.NoError(t, err)

	// Verify change
	value, err = reg.Get(ctx, "test_var")
	require.NoError(t, err)
	assert.Equal(t, "id_value", value)
}

func TestRegistry_Set_VariableWithoutName(t *testing.T) {
	reg, _, ctx := setupTestRegistry()
	require.NoError(t, reg.Start(ctx))
	defer safeStop(t, reg)

	// Setup storage
	storage := newMockStorage(map[string]string{
		"app:my_var": "initial_value",
	})
	storageID := registry.ParseID("app:storage")
	reg.storages.Store(storageID, storage)

	// Register variable without Name
	variable := env.Variable{
		Name:      "",
		StorageID: storageID,
		ReadOnly:  false,
	}
	reg.variablesByID.Store(variable.ID, variable)
	reg.variablesByName.Store("app:my_var", variable.ID)

	// Can set by full ID string
	err := reg.Set(ctx, "app:my_var", "new_value")
	require.NoError(t, err)

	// Verify change
	value, err := reg.Get(ctx, "app:my_var")
	require.NoError(t, err)
	assert.Equal(t, "new_value", value)
}

func TestRegistry_ReadOnlyVariable(t *testing.T) {
	reg, _, ctx := setupTestRegistry()
	require.NoError(t, reg.Start(ctx))
	defer safeStop(t, reg)

	// Setup storage
	storage := newMockStorage(map[string]string{
		"readonly_var": "readonly_value",
	})
	storageID := registry.ParseID("app:storage")
	reg.storages.Store(storageID, storage)

	// Register read-only variable
	variable := env.Variable{
		Name:      "readonly_var",
		StorageID: storageID,
		ReadOnly:  true,
	}
	reg.variablesByID.Store(variable.ID, variable)
	reg.variablesByName.Store("readonly_var", variable.ID)

	// Can read
	value, err := reg.Get(ctx, "readonly_var")
	require.NoError(t, err)
	assert.Equal(t, "readonly_value", value)

	// Cannot write
	err = reg.Set(ctx, "readonly_var", "new_value")
	require.Error(t, err)
	assert.Equal(t, env.ErrVariableReadOnly, err)
}

func TestRegistry_DefaultValue(t *testing.T) {
	reg, _, ctx := setupTestRegistry()
	require.NoError(t, reg.Start(ctx))
	defer safeStop(t, reg)

	// Setup storage without the variable
	storage := newMockStorage(map[string]string{})
	storageID := registry.ParseID("app:storage")
	reg.storages.Store(storageID, storage)

	// Register variable with default value
	variable := env.Variable{
		Name:         "default_var",
		StorageID:    storageID,
		DefaultValue: "default_value",
	}
	reg.variablesByID.Store(variable.ID, variable)
	reg.variablesByName.Store("default_var", variable.ID)

	// Should return default value when not in storage
	value, err := reg.Get(ctx, "default_var")
	require.NoError(t, err)
	assert.Equal(t, "default_value", value)

	// Should return storage value when present
	err = storage.Set(ctx, "default_var", "storage_value")
	require.NoError(t, err)
	value, err = reg.Get(ctx, "default_var")
	require.NoError(t, err)
	assert.Equal(t, "storage_value", value)
}

func TestRegistry_ErrorCases(t *testing.T) {
	reg, _, ctx := setupTestRegistry()
	require.NoError(t, reg.Start(ctx))
	defer safeStop(t, reg)

	t.Run("VariableNotFound", func(t *testing.T) {
		_, err := reg.Get(ctx, "nonexistent")
		require.Error(t, err)
		assert.Equal(t, env.ErrVariableNotFound, err)

		err = reg.Set(ctx, "nonexistent", "value")
		require.Error(t, err)
		assert.Equal(t, env.ErrVariableNotFound, err)
	})

	t.Run("StorageNotFound", func(t *testing.T) {
		// Register variable pointing to nonexistent storage
		variable := env.Variable{
			Name:      "test_var",
			StorageID: registry.ParseID("app:nonexistent"),
		}
		reg.variablesByID.Store(variable.ID, variable)
		reg.variablesByName.Store("test_var", variable.ID)

		_, err := reg.Get(ctx, "test_var")
		require.Error(t, err)
		assert.Equal(t, env.ErrStorageNotFound, err)

		err = reg.Set(ctx, "test_var", "value")
		require.Error(t, err)
		assert.Equal(t, env.ErrStorageNotFound, err)
	})
}

func TestRegistry_All(t *testing.T) {
	reg, _, ctx := setupTestRegistry()
	require.NoError(t, reg.Start(ctx))
	defer safeStop(t, reg)

	// Setup multiple storages
	storage1 := newMockStorage(map[string]string{
		"var1": "value1",
		"var2": "value2",
	})
	storage2 := newMockStorage(map[string]string{
		"var3": "value3",
	})

	reg.storages.Store(registry.ParseID("app:storage1"), storage1)
	reg.storages.Store(registry.ParseID("app:storage2"), storage2)

	// All() returns all variables from all storages
	all, err := reg.All(ctx)
	require.NoError(t, err)

	expected := map[string]string{
		"var1": "value1",
		"var2": "value2",
		"var3": "value3",
	}
	assert.Equal(t, expected, all)
}

func TestRegistry_Variable_Validation(t *testing.T) {
	tests := []struct {
		name      string
		variable  env.Variable
		wantError string
	}{
		{
			name: "valid_with_name",
			variable: env.Variable{
				Name:      "valid_name",
				StorageID: registry.ParseID("app:storage"),
			},
			wantError: "",
		},
		{
			name: "valid_without_name",
			variable: env.Variable{
				Name:      "",
				StorageID: registry.ParseID("app:storage"),
			},
			wantError: "",
		},
		{
			name: "invalid_name_dash",
			variable: env.Variable{
				Name:      "invalid-name",
				StorageID: registry.ParseID("app:storage"),
			},
			wantError: "must only contain alphanumeric",
		},
		{
			name: "invalid_name_space",
			variable: env.Variable{
				Name:      "invalid name",
				StorageID: registry.ParseID("app:storage"),
			},
			wantError: "must only contain alphanumeric",
		},
		{
			name: "empty_name_allowed",
			variable: env.Variable{
				Name:      "",
				StorageID: registry.ParseID("app:storage"),
			},
			wantError: "",
		},
		{
			name: "invalid_storage_id_empty_ns",
			variable: env.Variable{
				Name:      "valid_name",
				StorageID: registry.ID{NS: "", Name: "storage"},
			},
			wantError: "invalid storage ID format",
		},
		{
			name: "invalid_storage_id_empty_name",
			variable: env.Variable{
				Name:      "valid_name",
				StorageID: registry.ID{NS: "app", Name: ""},
			},
			wantError: "invalid storage ID format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.variable.Validate()
			if tt.wantError == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
			}
		})
	}
}

func TestRegistry_EventHandling_StorageRegister(t *testing.T) {
	reg, bus, ctx := setupTestRegistry()
	require.NoError(t, reg.Start(ctx))
	defer safeStop(t, reg)

	// Setup event listener for responses
	var responses []event.Event
	var mu sync.Mutex
	var wg sync.WaitGroup

	sub, err := eventbus.NewSubscriber(ctx, bus, env.System, "(accept|reject)", func(evt event.Event) {
		if evt.Kind == env.Accepted || evt.Kind == env.Rejected {
			mu.Lock()
			responses = append(responses, evt)
			mu.Unlock()
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, sub.ID())

	t.Run("ValidStorage", func(t *testing.T) {
		responses = nil
		wg.Add(1)

		storage := newMockStorage(nil)
		event := event.Event{
			System: env.System,
			Kind:   env.StorageRegister,
			Path:   "app:test-storage",
			Data:   storage,
		}

		bus.Send(ctx, event)

		// Wait for response
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		// Verify storage was registered
		stored, exists := reg.storages.Load(registry.ParseID("app:test-storage"))
		assert.True(t, exists)
		assert.Equal(t, storage, stored)

		// Verify accept event was sent
		mu.Lock()
		require.Len(t, responses, 1)
		assert.Equal(t, env.Accepted, responses[0].Kind)
		assert.Equal(t, event.Path, responses[0].Path)
		mu.Unlock()
	})

	t.Run("InvalidStorage", func(t *testing.T) {
		responses = nil
		wg.Add(1)

		event := event.Event{
			System: env.System,
			Kind:   env.StorageRegister,
			Path:   "app:invalid-storage",
			Data:   "invalid-storage", // Wrong type
		}

		bus.Send(ctx, event)

		// Wait for response
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		// Verify storage was not registered
		_, exists := reg.storages.Load(registry.ParseID("app:invalid-storage"))
		assert.False(t, exists)

		// Verify reject event was sent
		mu.Lock()
		require.Len(t, responses, 1)
		assert.Equal(t, env.Rejected, responses[0].Kind)
		assert.Equal(t, event.Path, responses[0].Path)
		assert.Contains(t, responses[0].Data.(string), "invalid storage data type")
		mu.Unlock()
	})
}

func TestRegistry_EventHandling_VariableRegister(t *testing.T) {
	reg, bus, ctx := setupTestRegistry()
	require.NoError(t, reg.Start(ctx))
	defer safeStop(t, reg)

	// Setup event listener for responses
	var responses []event.Event
	var mu sync.Mutex
	var wg sync.WaitGroup

	sub, err := eventbus.NewSubscriber(ctx, bus, env.System, "(accept|reject)", func(evt event.Event) {
		if evt.Kind == env.Accepted || evt.Kind == env.Rejected {
			mu.Lock()
			responses = append(responses, evt)
			mu.Unlock()
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, sub.ID())

	// First register storage
	storage := newMockStorage(nil)
	reg.storages.Store(registry.ParseID("app:storage"), storage)

	t.Run("ValidVariable", func(t *testing.T) {
		responses = nil
		wg.Add(1)

		variable := env.Variable{
			Name:      "test_var",
			StorageID: registry.ParseID("app:storage"),
		}
		event := event.Event{
			System: env.System,
			Kind:   env.VariableRegister,
			Path:   "app:test_var",
			Data:   variable,
		}

		bus.Send(ctx, event)

		// Wait for response
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		// Verify variable was registered by ID
		stored, exists := reg.variablesByID.Load(variable.ID)
		assert.True(t, exists)
		assert.Equal(t, variable, stored)

		// Verify variable was registered by name
		_, exists = reg.variablesByName.Load("test_var")
		assert.True(t, exists)

		// Verify accept event was sent
		mu.Lock()
		require.Len(t, responses, 1)
		assert.Equal(t, env.Accepted, responses[0].Kind)
		mu.Unlock()
	})

	t.Run("InvalidVariable", func(t *testing.T) {
		responses = nil
		wg.Add(1)

		variable := env.Variable{
			Name:      "invalid-name", // Invalid name with dash
			StorageID: registry.ParseID("app:storage"),
		}
		event := event.Event{
			System: env.System,
			Kind:   env.VariableRegister,
			Path:   "app:invalid",
			Data:   variable,
		}

		bus.Send(ctx, event)

		// Wait for response
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		// Verify variable was not registered
		_, exists := reg.variablesByID.Load(variable.ID)
		assert.False(t, exists)

		// Verify reject event was sent
		mu.Lock()
		require.Len(t, responses, 1)
		assert.Equal(t, env.Rejected, responses[0].Kind)
		assert.Contains(t, responses[0].Data.(string), "invalid variable")
		mu.Unlock()
	})

	t.Run("StorageNotFound", func(t *testing.T) {
		responses = nil
		wg.Add(1)

		variable := env.Variable{
			Name:      "test_var2",
			StorageID: registry.ParseID("app:nonexistent"), // Storage doesn't exist
		}
		event := event.Event{
			System: env.System,
			Kind:   env.VariableRegister,
			Path:   "app:test_var2",
			Data:   variable,
		}

		bus.Send(ctx, event)

		// Wait for response
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		// Verify variable was not registered
		_, exists := reg.variablesByID.Load(variable.ID)
		assert.False(t, exists)

		// Verify reject event was sent
		mu.Lock()
		require.Len(t, responses, 1)
		assert.Equal(t, env.Rejected, responses[0].Kind)
		assert.Contains(t, responses[0].Data.(string), "referenced storage not found")
		mu.Unlock()
	})

	t.Run("DuplicateVariableName", func(t *testing.T) {
		responses = nil
		wg.Add(1)

		// Register first variable
		variable1 := env.Variable{
			Name:      "same_name",
			StorageID: registry.ParseID("app:storage"),
		}
		reg.variablesByID.Store(variable1.ID, variable1)
		reg.variablesByName.Store("same_name", variable1.ID)

		// Try to register second variable with same name
		variable2 := env.Variable{
			Name:      "same_name", // Same name as first variable
			StorageID: registry.ParseID("app:storage"),
		}
		event := event.Event{
			System: env.System,
			Kind:   env.VariableRegister,
			Path:   "app:var2",
			Data:   variable2,
		}

		bus.Send(ctx, event)

		// Wait for response
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		// Verify second variable was not registered
		_, exists := reg.variablesByID.Load(variable2.ID)
		assert.False(t, exists)

		// Verify reject event was sent
		mu.Lock()
		require.Len(t, responses, 1)
		assert.Equal(t, env.Rejected, responses[0].Kind)
		assert.Contains(t, responses[0].Data.(string), "variable name already exists")
		mu.Unlock()
	})
}

func TestRegistry_EventHandling_VariableUpdate(t *testing.T) {
	reg, bus, ctx := setupTestRegistry()
	require.NoError(t, reg.Start(ctx))
	defer safeStop(t, reg)

	// Setup event listener for responses
	var responses []event.Event
	var mu sync.Mutex
	var wg sync.WaitGroup

	sub, err := eventbus.NewSubscriber(ctx, bus, env.System, "(accept|reject)", func(evt event.Event) {
		if evt.Kind == env.Accepted || evt.Kind == env.Rejected {
			mu.Lock()
			responses = append(responses, evt)
			mu.Unlock()
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, sub.ID())

	// Register storages
	storage1 := newMockStorage(nil)
	storage2 := newMockStorage(nil)
	reg.storages.Store(registry.ParseID("app:storage1"), storage1)
	reg.storages.Store(registry.ParseID("app:storage2"), storage2)

	// Register initial variable
	variable := env.Variable{
		Name:      "test_var",
		StorageID: registry.ParseID("app:storage1"),
	}
	reg.variablesByID.Store(variable.ID, variable)
	reg.variablesByName.Store("test_var", variable.ID)

	t.Run("ValidUpdate", func(t *testing.T) {
		responses = nil
		wg.Add(1)

		// Update variable to use different storage
		updatedVariable := env.Variable{
			Name:      "test_var",
			StorageID: registry.ParseID("app:storage2"), // Different storage
		}
		event := event.Event{
			System: env.System,
			Kind:   env.VariableUpdate,
			Path:   "app:test_var",
			Data:   updatedVariable,
		}

		bus.Send(ctx, event)

		// Wait for response
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		// Verify variable was updated
		stored, exists := reg.variablesByID.Load(variable.ID)
		require.True(t, exists)
		assert.Equal(t, updatedVariable, stored)

		// Verify accept event was sent
		mu.Lock()
		require.Len(t, responses, 1)
		assert.Equal(t, env.Accepted, responses[0].Kind)
		mu.Unlock()
	})
}

func TestRegistry_EventHandling_VariableDelete(t *testing.T) {
	reg, bus, ctx := setupTestRegistry()
	require.NoError(t, reg.Start(ctx))
	defer safeStop(t, reg)

	// Setup event listener for responses
	var responses []event.Event
	var mu sync.Mutex
	var wg sync.WaitGroup

	sub, err := eventbus.NewSubscriber(ctx, bus, env.System, "(accept|reject)", func(evt event.Event) {
		if evt.Kind == env.Accepted || evt.Kind == env.Rejected {
			mu.Lock()
			responses = append(responses, evt)
			mu.Unlock()
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, sub.ID())

	// Register variable
	variable := env.Variable{
		Name:      "test_var",
		StorageID: registry.ParseID("app:storage"),
	}
	reg.variablesByID.Store(variable.ID, variable)
	reg.variablesByName.Store("test_var", variable.ID)

	responses = nil
	wg.Add(1)

	event := event.Event{
		System: env.System,
		Kind:   env.VariableDelete,
		Path:   "app:test_var",
	}

	bus.Send(ctx, event)

	// Wait for response
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}

	// Verify variable was deleted
	_, exists := reg.variablesByID.Load(variable.ID)
	assert.False(t, exists)

	_, exists = reg.variablesByName.Load("test_var")
	assert.False(t, exists)

	// Verify accept event was sent
	mu.Lock()
	require.Len(t, responses, 1)
	assert.Equal(t, env.Accepted, responses[0].Kind)
	mu.Unlock()
}

func TestRegistry_EventHandling_StorageDelete(t *testing.T) {
	reg, bus, ctx := setupTestRegistry()
	require.NoError(t, reg.Start(ctx))
	defer safeStop(t, reg)

	// Setup event listener for responses
	var responses []event.Event
	var mu sync.Mutex
	var wg sync.WaitGroup

	sub, err := eventbus.NewSubscriber(ctx, bus, env.System, "(accept|reject)", func(evt event.Event) {
		if evt.Kind == env.Accepted || evt.Kind == env.Rejected {
			mu.Lock()
			responses = append(responses, evt)
			mu.Unlock()
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, sub.ID())

	// Register storage
	storage := newMockStorage(nil)
	storageID := registry.ParseID("app:test-storage")
	reg.storages.Store(storageID, storage)

	responses = nil
	wg.Add(1)

	event := event.Event{
		System: env.System,
		Kind:   env.StorageDelete,
		Path:   "app:test-storage",
	}

	bus.Send(ctx, event)

	// Wait for response
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}

	// Verify storage was deleted
	_, exists := reg.storages.Load(storageID)
	assert.False(t, exists)

	// Verify accept event was sent
	mu.Lock()
	require.Len(t, responses, 1)
	assert.Equal(t, env.Accepted, responses[0].Kind)
	mu.Unlock()
}

func TestRegistry_GetBaseName(t *testing.T) {
	reg, _, _ := setupTestRegistry()

	t.Run("WithName", func(t *testing.T) {
		variable := &env.Variable{
			Name: "my_var",
		}
		envName := reg.getEnvName(variable)
		assert.Equal(t, "my_var", envName)
	})

	t.Run("WithoutName", func(t *testing.T) {
		variable := &env.Variable{
			Name: "",
		}
		envName := reg.getEnvName(variable)
		assert.Equal(t, "app:test", envName)
	})
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	reg, _, ctx := setupTestRegistry()
	require.NoError(t, reg.Start(ctx))
	defer safeStop(t, reg)

	// Setup storage with thread-safe implementation
	storage := newMockStorage(map[string]string{
		"concurrent_var": "initial_value",
	})
	storageID := registry.ParseID("app:storage")
	reg.storages.Store(storageID, storage)

	// Register variable
	variable := env.Variable{
		Name:      "concurrent_var",
		StorageID: storageID,
		ReadOnly:  false,
	}
	reg.variablesByID.Store(variable.ID, variable)
	reg.variablesByName.Store("concurrent_var", variable.ID)

	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // Both read and write operations

	// Concurrent reads and writes
	for i := 0; i < numGoroutines; i++ {
		go func(_ int) {
			defer wg.Done()
			// Read operation
			_, err := reg.Get(ctx, "concurrent_var")
			assert.NoError(t, err)
		}(i)

		go func(idx int) {
			defer wg.Done()
			// Write operation
			err := reg.Set(ctx, "concurrent_var", fmt.Sprintf("value_%d", idx))
			assert.NoError(t, err)
		}(i)
	}

	wg.Wait()

	// Verify final state is consistent
	value, err := reg.Get(ctx, "concurrent_var")
	require.NoError(t, err)
	assert.NotEmpty(t, value)
}
