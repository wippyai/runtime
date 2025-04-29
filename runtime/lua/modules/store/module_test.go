package store

import (
	"context"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/store"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"
)

// mockResource implements the resource.Resource interface
type mockResource struct {
	resValue any
	released bool
}

func (m *mockResource) Get() (any, error) {
	return m.resValue, nil
}

func (m *mockResource) Release() {
	m.released = true
}

// mockResourceRegistry is a simple mock for the resource registry
type mockResourceRegistry struct {
	resources map[registry.ID]resource.Resource[any]
}

func (m *mockResourceRegistry) Acquire(
	_ context.Context,
	id registry.ID,
	_ resource.AccessMode,
) (resource.Resource[any], error) {
	res, ok := m.resources[id]
	if !ok {
		return nil, resource.ErrResourceNotFound
	}
	return res, nil
}

func (m *mockResourceRegistry) List() ([]registry.ID, error) {
	ids := make([]registry.ID, 0, len(m.resources))
	for id := range m.resources {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *mockResourceRegistry) Exists(id registry.ID) bool {
	_, ok := m.resources[id]
	return ok
}

// mockStore implements the store.Store interface for testing
type mockStore struct {
	data  map[string]payload.Payload
	err   error // optional error to simulate failures
	clock time.Time
}

// NewMockStore creates a new test store with initial test data
//
//nolint:revive
func NewMockStore() *mockStore {
	return &mockStore{
		data:  make(map[string]payload.Payload),
		clock: time.Now(),
	}
}

func (m *mockStore) Get(_ context.Context, key registry.ID) (payload.Payload, error) {
	if m.err != nil {
		return nil, m.err
	}

	keyStr := key.String()
	if val, exists := m.data[keyStr]; exists {
		return val, nil
	}
	return nil, store.ErrKeyNotFound
}

func (m *mockStore) Set(_ context.Context, entry store.Entry) error {
	if m.err != nil {
		return m.err
	}

	keyStr := entry.Key.String()
	m.data[keyStr] = entry.Value
	return nil
}

func (m *mockStore) Delete(_ context.Context, key registry.ID) error {
	if m.err != nil {
		return m.err
	}

	keyStr := key.String()
	if _, exists := m.data[keyStr]; !exists {
		return store.ErrKeyNotFound
	}

	delete(m.data, keyStr)
	return nil
}

func (m *mockStore) Has(_ context.Context, key registry.ID) (bool, error) {
	if m.err != nil {
		return false, m.err
	}

	keyStr := key.String()
	_, exists := m.data[keyStr]
	return exists, nil
}

// setupLuaWithStore sets up a Lua state with our store module and a connected store
func setupLuaWithStore(t *testing.T, mockRes *mockResource) (*engine.CoroutineVM, *lua.LState, engine.UnitOfWork, *engine.Runner) {
	logger := zaptest.NewLogger(t)

	// Create the store module
	module := NewStoreModule(logger)

	// Create a mock resource registry with our test store
	mockRegistry := &mockResourceRegistry{
		resources: map[registry.ID]resource.Resource[any]{
			registry.ParseID("app:test_store"): mockRes,
		},
	}

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Register the store module
	L.PreloadModule(module.Name(), module.Loader)

	// Set up a transcoder to convert between Lua and Go values
	// This is a simplified version for testing - in a real environment,
	// a full transcoder implementation would be available
	dtt := setupTestTranscoder()
	ctx := payload.WithTranscoder(context.Background(), dtt)

	// Create a runner with the coroutine layer
	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

	// Create a UOW for resource management
	uw, ctx := runner.InitUnitOfWork(ctx)

	// Add the resource registry to the context
	ctx = resource.WithResources(ctx, mockRegistry)

	// Set the context in the Lua state
	L.SetContext(ctx)

	return vm, L, uw, runner
}

// setupTestTranscoder creates a minimal transcoder for testing
// In a production environment, this would be a full implementation
type testTranscoder struct{}

func setupTestTranscoder() payload.Transcoder {
	return &testTranscoder{}
}

func (t *testTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	switch format {
	case payload.Lua:
		// Convert Go to Lua
		data := p.Data()
		var luaVal lua.LValue

		switch v := data.(type) {
		case nil:
			luaVal = lua.LNil
		case string:
			luaVal = lua.LString(v)
		case int:
			luaVal = lua.LNumber(v)
		case float64:
			luaVal = lua.LNumber(v)
		case bool:
			luaVal = lua.LBool(v)
		default:
			// For simplicity, convert unhandled types to string
			luaVal = lua.LString("converted value")
		}
		return payload.NewPayload(luaVal, payload.Lua), nil
	case payload.Golang:
		// Convert Lua to Go
		luaVal, ok := p.Data().(lua.LValue)
		if !ok {
			return nil, nil
		}

		var goVal interface{}
		switch v := luaVal.(type) {
		case lua.LBool:
			goVal = bool(v)
		case lua.LString:
			goVal = string(v)
		case lua.LNumber:
			goVal = float64(v)
		default:
			// For simplicity, convert unhandled types to string
			goVal = "converted value"
		}
		return payload.New(goVal), nil
	}
	return p, nil
}

func (t *testTranscoder) Unmarshal(_ payload.Payload, _ interface{}) error {
	// Simple implementation for testing
	return nil
}

// TestStoreBasicGet tests the store.get function retrieves a store correctly
func TestStoreBasicGet(t *testing.T) {
	// Create a mock store for testing
	mockStoreObj := NewMockStore()

	// Add some initial data
	testKey := registry.ParseID("test:key1")
	mockStoreObj.data[testKey.String()] = payload.New("test value")

	// Create our resource that will be tracked for release
	mockRes := &mockResource{
		resValue: mockStoreObj,
	}

	// Setup Lua with the test store
	vm, L, uw, runner := setupLuaWithStore(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import our test function
	err := vm.Import(`
		function test_store_get()
			local store = require("store")
			local s = store.get("app:test_store")
			
			-- Test the connection is valid
			local result = {}
			
			-- Release the store
			local ok = s:release()
			
			return ok
		end
	`, "test", "test_store_get")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_store_get")
	require.NoError(t, err, "Lua execution failed")

	assert.Equal(t, lua.LTrue, result, "Store.get should return true on successful release")
	assert.True(t, mockRes.released, "Store resource was not released")
}

// TestStoreGetValue tests the store:get method retrieves a value correctly
func TestStoreGetValue(t *testing.T) {
	// Create a mock store for testing
	mockStoreObj := NewMockStore()

	// Add some initial data
	testKey := registry.ParseID("test:key1")
	mockStoreObj.data[testKey.String()] = payload.New("test value")

	// Create our resource
	mockRes := &mockResource{
		resValue: mockStoreObj,
	}

	// Setup Lua with the test store
	vm, L, uw, runner := setupLuaWithStore(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import our test function
	err := vm.Import(`
		function test_store_get_value()
			local store = require("store")
			local s = store.get("app:test_store")
			
			-- Get a value
			local value, err = s:get("test:key1")
			if err then error(err) end
			
			-- Get a non-existent key
			local missing, err = s:get("test:nonexistent")
			
			-- Release the store
			s:release()
			
			return {
				value = value,
				has_error = err ~= nil,
				missing_value = missing,
				missing_error = missing == nil and err ~= nil
			}
		end
	`, "test", "test_store_get_value")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_store_get_value")
	require.NoError(t, err, "Lua execution failed")

	resultTable := result.(*lua.LTable)
	value := resultTable.RawGetString("value")
	missingErr := resultTable.RawGetString("missing_error")

	assert.Equal(t, lua.LString("test value"), value, "Retrieved incorrect value")
	assert.Equal(t, lua.LBool(true), missingErr, "No error on missing key")
}

// TestStoreSetValue tests the store:set method stores a value correctly
func TestStoreSetValue(t *testing.T) {
	// Create a mock store for testing
	mockStoreObj := NewMockStore()

	// Create our resource
	mockRes := &mockResource{
		resValue: mockStoreObj,
	}

	// Setup Lua with the test store
	vm, L, uw, runner := setupLuaWithStore(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import our test function
	err := vm.Import(`
		function test_store_set_value()
			local store = require("store")
			local s = store.get("app:test_store")
			
			-- Set a value
			local ok, err = s:set("test:key2", "new value")
			if err then error(err) end
			
			-- Retrieve the set value
			local value, err = s:get("test:key2")
			if err then error(err) end
			
			-- Set with TTL (not actually testing expiration, just syntax)
			local okTtl, errTtl = s:set("test:key3", "ttl value", 60)
			
			-- Release the store
			s:release()
			
			return {
				success = ok,
				value = value,
				ttl_success = okTtl
			}
		end
	`, "test", "test_store_set_value")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_store_set_value")
	require.NoError(t, err, "Lua execution failed")

	resultTable := result.(*lua.LTable)
	success := resultTable.RawGetString("success")
	value := resultTable.RawGetString("value")
	ttlSuccess := resultTable.RawGetString("ttl_success")

	assert.Equal(t, lua.LTrue, success, "Set operation should succeed")
	assert.Equal(t, lua.LString("new value"), value, "Retrieved incorrect value after set")
	assert.Equal(t, lua.LTrue, ttlSuccess, "Set with TTL should succeed")

	// Verify data was stored in the mock store
	assert.Equal(t, "new value", mockStoreObj.data[registry.ParseID("test:key2").String()].Data())
	assert.Equal(t, "ttl value", mockStoreObj.data[registry.ParseID("test:key3").String()].Data())
}

// TestStoreDelete tests the store:delete method removes a value correctly
func TestStoreDelete(t *testing.T) {
	// Create a mock store for testing
	mockStoreObj := NewMockStore()

	// Add some initial data
	testKey := registry.ParseID("test:key1")
	mockStoreObj.data[testKey.String()] = payload.New("test value")

	// Create our resource
	mockRes := &mockResource{
		resValue: mockStoreObj,
	}

	// Setup Lua with the test store
	vm, L, uw, runner := setupLuaWithStore(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import our test function
	err := vm.Import(`
		function test_store_delete()
			local store = require("store")
			local s = store.get("app:test_store")
			
			-- Delete an existing key
			local existsBeforeDelete, err = s:has("test:key1")
			if err then error(err) end
			
			local ok, err = s:delete("test:key1")
			if err then error(err) end
			
			local existsAfterDelete, err = s:has("test:key1")
			if err then error(err) end
			
			-- Delete a non-existent key
			local notOk, err = s:delete("test:nonexistent")
			
			-- Release the store
			s:release()
			
			return {
				before_delete = existsBeforeDelete,
				success = ok,
				after_delete = existsAfterDelete,
				nonexistent_success = notOk
			}
		end
	`, "test", "test_store_delete")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_store_delete")
	require.NoError(t, err, "Lua execution failed")

	resultTable := result.(*lua.LTable)
	beforeDelete := resultTable.RawGetString("before_delete")
	success := resultTable.RawGetString("success")
	afterDelete := resultTable.RawGetString("after_delete")
	nonexistentSuccess := resultTable.RawGetString("nonexistent_success")

	assert.Equal(t, lua.LBool(true), beforeDelete, "Key should exist before delete")
	assert.Equal(t, lua.LTrue, success, "Delete operation should succeed")
	assert.Equal(t, lua.LBool(false), afterDelete, "Key should not exist after delete")
	assert.Equal(t, lua.LBool(false), nonexistentSuccess, "Delete of non-existent key should return false")

	// Verify key was removed from the mock store
	_, exists := mockStoreObj.data[testKey.String()]
	assert.False(t, exists, "Key should be removed from store")
}

// TestStoreHas tests the store:has method checks key existence correctly
func TestStoreHas(t *testing.T) {
	// Create a mock store for testing
	mockStoreObj := NewMockStore()

	// Add some initial data
	testKey := registry.ParseID("test:key1")
	mockStoreObj.data[testKey.String()] = payload.New("test value")

	// Create our resource
	mockRes := &mockResource{
		resValue: mockStoreObj,
	}

	// Setup Lua with the test store
	vm, L, uw, runner := setupLuaWithStore(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import our test function
	err := vm.Import(`
		function test_store_has()
			local store = require("store")
			local s = store.get("app:test_store")
			
			-- Check an existing key
			local exists, err = s:has("test:key1")
			if err then error(err) end
			
			-- Check a non-existent key
			local notExists, err = s:has("test:nonexistent")
			if err then error(err) end
			
			-- Release the store
			s:release()
			
			return {
				exists = exists,
				not_exists = notExists
			}
		end
	`, "test", "test_store_has")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_store_has")
	require.NoError(t, err, "Lua execution failed")

	resultTable := result.(*lua.LTable)
	exists := resultTable.RawGetString("exists")
	notExists := resultTable.RawGetString("not_exists")

	assert.Equal(t, lua.LBool(true), exists, "Existing key should be found")
	assert.Equal(t, lua.LBool(false), notExists, "Non-existent key should not be found")
}

// TestStoreGetError tests error handling when store.get fails
func TestStoreGetError(t *testing.T) {
	// Create a mock resource registry with no resources - will trigger "not found" error
	mockRegistry := &mockResourceRegistry{
		resources: map[registry.ID]resource.Resource[any]{},
	}

	logger := zaptest.NewLogger(t)
	module := NewStoreModule(logger)

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Register the store module
	L.PreloadModule(module.Name(), module.Loader)

	// Set up the context
	ctx := context.Background()
	dtt := setupTestTranscoder()
	ctx = payload.WithTranscoder(ctx, dtt)

	// Create a runner with the coroutine layer
	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

	// Create a UOW for resource management
	uw, ctx := runner.InitUnitOfWork(ctx)
	defer func() {
		err := uw.Close()
		assert.NoError(t, err)
	}()

	// Add the empty resource registry to the context
	ctx = resource.WithResources(ctx, mockRegistry)

	// Set the context in the Lua state
	L.SetContext(ctx)

	// Import our test function that should trigger an error
	err = vm.Import(`
		function test_store_get_error()
			local store = require("store")
			local ok, err = pcall(function()
				local s = store.get("nonexistent:store")
				return s
			end)
			return {success = ok, error = not ok}
		end
	`, "test", "test_store_get_error")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function
	result, err := runner.Execute(L.Context(), "test_store_get_error")
	require.NoError(t, err, "Lua execution failed")

	resultTable := result.(*lua.LTable)
	success := resultTable.RawGetString("success")
	hasError := resultTable.RawGetString("error")

	assert.Equal(t, lua.LBool(false), success, "Function should fail")
	assert.Equal(t, lua.LBool(true), hasError, "Error should be returned")
}
