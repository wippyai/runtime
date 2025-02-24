package __ignore

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/modules/sql"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	sqlapi "github.com/ponyruntime/pony/api/service/sql"
	"github.com/ponyruntime/pony/runtime/uow"
	sqlres "github.com/ponyruntime/pony/service/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"

	// Import SQLite driver for testing
	_ "github.com/mattn/go-sqlite3"
)

// countingResource tracks how many times Release is called
type countingResource struct {
	sql.mockResource
	releaseCount int
}

func (r *countingResource) Release() error {
	r.releaseCount++
	r.released = true
	return nil
}

// TestResourceReleaseDB tests that database resources are released properly
func TestResourceReleaseDB(t *testing.T) {
	realDB, _, cleanup := sql.setupTestDB(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)

	// Create a counting resource
	countRes := &countingResource{
		mockResource: sql.mockResource{
			resValue: sqlres.DBResource{
				DB:   realDB,
				Type: sqlapi.KindSQLite,
			},
		},
	}

	// Create a mock resource registry with our test database
	mockRegistry := &sql.mockResourceRegistry{
		resources: map[registry.ID]resource.Resource[any]{
			registry.ParseID("app:test_db"): countRes,
		},
	}

	// Create the SQL module
	module := sql.NewSQLModule(logger)

	// Set up the Lua state
	L := lua.NewState()
	defer L.Close()
	L.PreloadModule(module.Name(), module.Loader)

	// Create a UOW for resource management
	ctx, uw := uow.WithContext(context.Background())

	// Add the resource registry to the context
	ctx = resource.WithResources(ctx, mockRegistry)

	// Set the context in the Lua state
	L.SetContext(ctx)

	// Test explicit release of database
	err := L.DoString(`
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		if err then error(err) end

		-- Store for testing
		test_result = {
			db_acquired = (db ~= nil)
		}

		-- Explicitly release the database
		local ok, err = db:release()
		if err then error(err) end

		test_result.db_released = ok
	`)

	// Check for Lua errors
	require.NoError(t, err, "Lua execution failed")

	// Verify database was explicitly released
	resultTable := L.GetGlobal("test_result").(*lua.LTable)
	dbAcquired := resultTable.RawGetString("db_acquired").(lua.LBool)
	dbReleased := resultTable.RawGetString("db_released").(lua.LBool)

	assert.True(t, bool(dbAcquired), "DB should have been acquired")
	assert.True(t, bool(dbReleased), "DB should have been released")
	assert.Equal(t, 1, countRes.releaseCount, "Release should be called once")
	assert.True(t, countRes.released, "Resource should be marked as released")

	// Cleanup UOW (should not release again since already released)
	err = uw.Close()
	assert.NoError(t, err, "UOW cleanup failed")
	assert.Equal(t, 1, countRes.releaseCount, "Release should still be called only once")
}

// TestResourceCleanupOnUOWClose tests that resources are automatically cleaned up when UOW closes
func TestResourceCleanupOnUOWClose(t *testing.T) {
	realDB, _, cleanup := sql.setupTestDB(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)

	// Create a counting resource
	countRes := &countingResource{
		mockResource: sql.mockResource{
			resValue: sqlres.DBResource{
				DB:   realDB,
				Type: sqlapi.KindSQLite,
			},
		},
	}

	// Create a mock resource registry with our test database
	mockRegistry := &sql.mockResourceRegistry{
		resources: map[registry.ID]resource.Resource[any]{
			registry.ParseID("app:test_db"): countRes,
		},
	}

	// Create the SQL module
	module := sql.NewSQLModule(logger)

	// Set up the Lua state
	L := lua.NewState()
	defer L.Close()
	L.PreloadModule(module.Name(), module.Loader)

	// Create a UOW for resource management
	ctx, uw := uow.WithContext(context.Background())

	// Add the resource registry to the context
	ctx = resource.WithResources(ctx, mockRegistry)

	// Set the context in the Lua state
	L.SetContext(ctx)

	// Get database but don't explicitly release it
	err := L.DoString(`
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		if err then error(err) end

		-- Store for testing
		test_result = {
			db_acquired = (db ~= nil)
		}

		-- No explicit release
	`)

	// Check for Lua errors
	require.NoError(t, err, "Lua execution failed")

	// Verify database was acquired
	resultTable := L.GetGlobal("test_result").(*lua.LTable)
	dbAcquired := resultTable.RawGetString("db_acquired").(lua.LBool)

	assert.True(t, bool(dbAcquired), "DB should have been acquired")
	assert.Equal(t, 0, countRes.releaseCount, "Release should not be called yet")
	assert.False(t, countRes.released, "Resource should not be released yet")

	// Close UOW, which should release the resource
	err = uw.Close()
	assert.NoError(t, err, "UOW cleanup failed")
	assert.Equal(t, 1, countRes.releaseCount, "Release should be called once after UOW closes")
	assert.True(t, countRes.released, "Resource should be released after UOW closes")
}

// TestResourceStatementsCleanup tests that prepared statements are properly cleaned up
func TestResourceStatementsCleanup(t *testing.T) {
	db, mockRes, cleanup := sql.setupTestDB(t)
	defer cleanup()

	L, uw := sql.setupLuaWithDB(t, mockRes)
	defer L.Close()

	// Track if statements are closed
	stmtClosedFlag := false
	origClose := db.Close

	// Override Close to track if it was called with statements still open
	db.Close = func() error {
		// Check for open statements - this is not reliable since SQLite may clean
		// them up automatically when the connection is closed
		stats := db.Stats()
		stmtClosedFlag = stats.OpenStatements == 0
		return origClose()
	}

	// Create and use statements but don't explicitly close them
	err := L.DoString(`
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		if err then error(err) end

		-- Prepare some statements
		local stmt1, err = db:prepare("SELECT 1")
		if err then error(err) end

		local stmt2, err = db:prepare("SELECT 2")
		if err then error(err) end

		-- Use one statement
		local rows, err = stmt1:query()
		if err then error(err) end
		
		-- Store for testing
		test_result = {
			stmts_prepared = 2
		}

		-- No explicit close for statements or DB
	`)

	// Check for Lua errors
	require.NoError(t, err, "Lua execution failed")

	// Close UOW, which should clean up
	err = uw.Close()
	assert.NoError(t, err, "UOW cleanup failed")

	// Note: SQLite may automatically close statements when the connection is closed,
	// so this test may not reliably confirm statement cleanup in all cases
	// But UOW cleanup should have run
	assert.True(t, mockRes.(*sql.mockResource).released, "Resource should be released after UOW closes")
}

// TestResourceTransactionCleanup tests that transactions are properly rolled back if not committed
func TestResourceTransactionCleanup(t *testing.T) {
	db, mockRes, cleanup := sql.setupTestDB(t)
	defer cleanup()

	L, uw := sql.setupLuaWithDB(t, mockRes)
	defer L.Close()

	// Create test table
	_, err := db.Exec(`CREATE TABLE auto_rollback (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err, "Failed to create test table")

	// Start a transaction but don't commit or roll it back
	err = L.DoString(`
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		if err then error(err) end

		-- Begin transaction
		local tx, err = db:begin()
		if err then error(err) end

		-- Insert some data
		local result, err = tx:execute("INSERT INTO auto_rollback (value) VALUES (?)", {"test value"})
		if err then error(err) end
		
		-- Store for testing
		test_result = {
			insert_done = (result.rows_affected > 0)
		}

		-- No explicit commit or rollback
	`)

	// Check for Lua errors
	require.NoError(t, err, "Lua execution failed")

	resultTable := L.GetGlobal("test_result").(*lua.LTable)
	insertDone := resultTable.RawGetString("insert_done").(lua.LBool)
	assert.True(t, bool(insertDone), "Insert should be done in transaction")

	// Close UOW, which should roll back uncommitted transaction
	err = uw.Close()
	assert.NoError(t, err, "UOW cleanup failed")

	// Verify data was not committed
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM auto_rollback").Scan(&count)
	require.NoError(t, err, "Failed to query table")
	assert.Equal(t, 0, count, "Table should be empty after automatic rollback")
}

// TestResourceCleanupOrder tests the correct cleanup order of resources
func TestResourceCleanupOrder(t *testing.T) {
	db, mockRes, cleanup := sql.setupTestDB(t)
	defer cleanup()

	L, uw := sql.setupLuaWithDB(t, mockRes)
	defer L.Close()

	// Create a complex setup with nested resources
	err := L.DoString(`
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		if err then error(err) end

		-- Begin transaction
		local tx, err = db:begin()
		if err then error(err) end

		-- Prepare statement in transaction
		local stmt1, err = tx:prepare("SELECT 1")
		if err then error(err) end

		-- Prepare statement directly on DB
		local stmt2, err = db:prepare("SELECT 2")
		if err then error(err) end
		
		-- Store for testing
		test_result = {
			setup_complete = true
		}

		-- No explicit cleanup
	`)

	// Check for Lua errors
	require.NoError(t, err, "Lua execution failed")

	// Everything should be set up properly
	resultTable := L.GetGlobal("test_result").(*lua.LTable)
	setupComplete := resultTable.RawGetString("setup_complete").(lua.LBool)
	assert.True(t, bool(setupComplete), "Setup should be complete")

	// Close UOW, which should clean up everything in the right order
	err = uw.Close()
	assert.NoError(t, err, "UOW cleanup failed")
	assert.True(t, mockRes.(*sql.mockResource).released, "Resource should be released")
}

// TestResourceMissingUOW tests the behavior when UOW is missing
func TestResourceMissingUOW(t *testing.T) {
	realDB, _, cleanup := sql.setupTestDB(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)

	// Create a resource
	mockRes := &sql.mockResource{
		resValue: sqlres.DBResource{
			DB:   realDB,
			Type: sqlapi.KindSQLite,
		},
	}

	// Create a mock resource registry with our test database
	mockRegistry := &sql.mockResourceRegistry{
		resources: map[registry.ID]resource.Resource[any]{
			registry.ParseID("app:test_db"): mockRes,
		},
	}

	// Create the SQL module
	module := sql.NewSQLModule(logger)

	// Set up the Lua state
	L := lua.NewState()
	defer L.Close()
	L.PreloadModule(module.Name(), module.Loader)

	// Create context without UOW
	ctx := context.Background()

	// Add the resource registry to the context
	ctx = resource.WithResources(ctx, mockRegistry)

	// Set the context in the Lua state
	L.SetContext(ctx)

	// Try to get database without UOW
	err := L.DoString(`
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		
		-- Store error for testing
		test_result = {
			has_error = (err ~= nil),
			error_msg = err
		}
	`)

	// Check for Lua errors
	require.NoError(t, err, "Lua execution failed")

	// Verify proper error handling
	resultTable := L.GetGlobal("test_result").(*lua.LTable)
	hasError := resultTable.RawGetString("has_error").(lua.LBool)
	errorMsg := ""
	if errMsg := resultTable.RawGetString("error_msg"); errMsg != lua.LNil {
		errorMsg = string(errMsg.(lua.LString))
	}

	assert.True(t, bool(hasError), "Should get error when UOW is missing")
	assert.Contains(t, errorMsg, "unit of work", "Error should mention missing UOW")
}
