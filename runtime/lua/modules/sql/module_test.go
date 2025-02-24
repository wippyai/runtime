package sql

import (
	"context"
	"database/sql"
	sqlapi "github.com/ponyruntime/pony/api/service/sql"
	sqlres "github.com/ponyruntime/pony/service/sql"
	"github.com/stretchr/testify/assert"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/uow"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"

	// Import SQLite driver for testing
	_ "github.com/mattn/go-sqlite3"
)

// mockResource implements the resource.Resource interface
type mockResource struct {
	resValue any
	released bool
}

func (m *mockResource) Get() (any, error) {
	return m.resValue, nil
}

func (m *mockResource) Release() error {
	m.released = true
	return nil
}

func (m *mockResource) Mode() resource.AccessMode {
	return resource.ModeNormal
}

// mockResourceRegistry is a simple mock for the resource registry
type mockResourceRegistry struct {
	resources map[registry.ID]resource.Resource[any]
}

func (m *mockResourceRegistry) Acquire(
	ctx context.Context,
	id registry.ID,
	mode resource.AccessMode,
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

// setupLuaWithDB sets up a Lua state with our SQL module and a connected database
func setupLuaWithDB(t *testing.T, mockRes *mockResource) (*engine.CoroutineVM, *lua.LState, *uow.UnitOfWork, *engine.Runner) {
	logger := zaptest.NewLogger(t)

	// Create the SQL module
	module := NewSQLModule(logger)

	// Create a mock resource registry with our test database
	mockRegistry := &mockResourceRegistry{
		resources: map[registry.ID]resource.Resource[any]{
			registry.ParseID("app:test_db"): mockRes,
		},
	}

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Register the SQL module
	L.PreloadModule(module.Name(), module.Loader)

	// Create a runner with the coroutine layer
	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

	// Create a UOW for resource management
	ctx, uw := uow.WithContext(context.Background())

	// Add the resource registry to the context
	ctx = resource.WithResources(ctx, mockRegistry)

	// Set the context in the Lua state
	L.SetContext(ctx)

	return vm, L, uw, runner
}

// TestModuleBasicDBGet tests the sql.get function with a basic SQLite database
func TestModuleBasicDBGet(t *testing.T) {
	// Create a SQLite in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err, "Failed to open SQLite database")
	defer func() {
		err := db.Close()
		assert.NoError(t, err, "Failed to close SQLite database")
	}()

	// Create a simple table for testing
	_, err = db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)")
	require.NoError(t, err, "Failed to create test table")

	// Create our resource that will be tracked for release
	mockRes := &mockResource{
		// Use the actual DBResource struct from the sql service package
		resValue: sqlres.DBResource{
			DB:   db,
			Type: sqlapi.KindSQLite,
		},
	}

	// Setup Lua with the test database using the helper function
	vm, L, uw, runner := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import our test function into the VM
	err = vm.Import(`
		function test_db_get()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then 
				print("Error getting DB:", err)
				error(err) 
			end

			-- Check database type
			local dbType, err = db:type()
			if err then
				print("Error getting DB type:", err)
				error(err) 
			end
			
			-- Store results for test verification
			local result = {
				db_type = dbType
			}
			
			-- Release the database
			local ok, err = db:release()
			if err then 
				print("Error releasing DB:", err)
				error(err) 
			end

			return result
		end
	`, "test", "test_db_get")
	require.NoError(t, err, "Failed to import test function")

	// Execute the function using the runner
	result, err := runner.Execute(L.Context(), "test_db_get")
	require.NoError(t, err, "Lua execution failed")

	// Verify the database type
	resultTable := result.(*lua.LTable)
	dbType := resultTable.RawGetString("db_type").(lua.LString)

	assert.Equal(t, "sqlite", string(dbType), "Incorrect database type returned")

	// Verify that the resource was released
	assert.True(t, mockRes.released, "Database resource was not released")
}
