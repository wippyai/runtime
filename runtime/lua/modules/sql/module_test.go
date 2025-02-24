package sql

import (
	"context"
	"database/sql"
	sqlapi "github.com/ponyruntime/pony/api/service/sql"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/runtime/uow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"

	// Import SQLite driver for testing
	_ "github.com/mattn/go-sqlite3"
)

// mockSQLResource is a simple mock for the SQL resource
type mockSQLResource struct {
	db   *sql.DB
	kind registry.Kind
}

func (m *mockSQLResource) DB() *sql.DB {
	return m.db
}

func (m *mockSQLResource) Type() registry.Kind {
	return m.kind
}

// mockResource implements the resource.Resource interface
type mockResource struct {
	resValue any
}

func (m *mockResource) Get() (any, error) {
	return m.resValue, nil
}

func (m *mockResource) Release() error {
	return nil
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

// TestModuleBasicDBGet tests the sql.get function with a basic SQLite database
func TestModuleBasicDBGet(t *testing.T) {
	// Create a test logger
	logger := zaptest.NewLogger(t)

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

	// Create the SQL module
	module := NewSQLModule(logger)

	// Create a mock resource registry with our test database
	mockRegistry := &mockResourceRegistry{
		resources: map[registry.ID]resource.Resource[any]{
			registry.ID{Name: "test_db"}: &mockResource{
				resValue: &mockSQLResource{
					db:   db,
					kind: sqlapi.KindSQLite,
				},
			},
		},
	}

	// Set up the Lua state
	L := lua.NewState()
	defer L.Close()

	// Register the SQL module
	L.PreloadModule(module.Name(), module.Loader)

	// Create a simple UOW for resource management
	ctx, uw := uow.WithContext(context.Background())

	// Add the resource registry to the context
	ctx = resource.WithResources(ctx, mockRegistry)

	// Set the context in the Lua state
	L.SetContext(ctx)

	// Load the SQL module in Lua
	err = L.DoString(`
		local sql = require("sql")
		local db, err = sql.get("test_db")
		if err then error(err) end
		
		-- Check database type
		local dbType, err = db:type()
		if err then error(err) end
		
		-- Store results for test verification
		test_result = {
			db_type = dbType
		}
		
		-- Release the database
		local ok, err = db:release()
		if err then error(err) end
	`)

	// Check for Lua errors
	require.NoError(t, err, "Lua execution failed")

	// Verify the database type
	resultTable := L.GetGlobal("test_result").(*lua.LTable)
	dbType := resultTable.RawGetString("db_type").(lua.LString)

	assert.Equal(t, "sqlite", string(dbType), "Incorrect database type returned")

	// Verify UOW cleanups executed correctly
	err = uw.Close()
	assert.NoError(t, err, "Unit of work cleanup failed")
}
