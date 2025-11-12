package sql

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ponyruntime/pony/api/resource"
	sqlapi "github.com/ponyruntime/pony/api/service/sql"

	_ "github.com/mattn/go-sqlite3"
	sqlres "github.com/ponyruntime/pony/service/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

// setupTestDB creates a shared in-memory SQLite database with a simple table.
// Using the connection string "file:memdb1?mode=memory&cache=shared" ensures the same
// connection is used for all operations.

//nolint:unparam // used in tests
func setupTestDBWithTestTable(t *testing.T) (*sql.DB, *mockResource, func()) {
	db, err := sql.Open("sqlite3", "file:memdb1?mode=memory&cache=shared")
	require.NoError(t, err)

	// Create table "test" and insert a row.
	_, err = db.ExecContext(t.Context(), "CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "INSERT INTO test (value) VALUES ('hello')")
	require.NoError(t, err)

	mockRes := &mockResource{
		resValue: sqlres.DBResource{
			DB:   db,
			Type: sqlapi.KindSQLite,
		},
	}
	cleanup := func() {
		err := db.Close()
		assert.NoError(t, err)
	}
	return db, mockRes, cleanup
}

// Note: The helpers setupTestDB(t *testing.T) and setupLuaWithDB(t, mockRes)
// are assumed to be defined in your module_test.go file. They provide a Lua VM
// (with the SQL module preloaded) and a runner with proper UOW context.
// For these tests, we reuse setupLuaWithDB directly.

// TestDBType verifies that db:type() returns the correct database type.
func TestDBType(t *testing.T) {
	_, mockRes, cleanup := setupTestDBWithTestTable(t)
	defer cleanup()

	vm, uw, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() { _ = uw.Close() }()

	err := vm.Import(`
		function test_db_type()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end
			local t, err = db:type()
			if err then error(err) end
			return t
		end
	`, "test", "test_db_type")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_db_type")
	require.NoError(t, err)
	assert.Equal(t, lua.LString("sqlite"), result)
}

// TestDBQuery verifies that db:query() returns expected rows.
func TestDBQuery(t *testing.T) {
	_, mockRes, cleanup := setupTestDBWithTestTable(t)
	defer cleanup()

	vm, uw, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() { _ = uw.Close() }()

	err := vm.Import(`
		function test_db_query()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end
			local rows, err = db:query("SELECT value FROM test")
			if err then error(err) end
			return { count = #rows, first = rows[1] and rows[1].value or nil }
		end
	`, "test", "test_db_query")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_db_query")
	require.NoError(t, err)
	resultTable, ok := result.(*lua.LTable)
	require.True(t, ok)

	count := resultTable.RawGetString("count")
	first := resultTable.RawGetString("first")
	assert.Equal(t, lua.LNumber(1), count)
	assert.Equal(t, lua.LString("hello"), first)
}

// TestDBExecute verifies that db:execute() updates data as expected.
func TestDBExecute(t *testing.T) {
	// Set up a separate shared in-memory DB for the execute test.
	db, err := sql.Open("sqlite3", "file:memdb2?mode=memory&cache=shared")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(t.Context(), "CREATE TABLE exec_test (id INTEGER PRIMARY KEY, value TEXT)")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "INSERT INTO exec_test (value) VALUES ('old')")
	require.NoError(t, err)

	// Wrap the DB in a mockResource so that setupLuaWithDB can use it.
	mockRes := &mockResource{
		resValue: sqlres.DBResource{
			DB:   db,
			Type: "sqlite",
		},
	}

	vm, uw, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Preserve resources from context before closing UOW
	// runner.Execute creates its own UOW, so we need to close the initial one
	// but preserve resources in a new context
	mockRegistry := resource.GetResources(ctx)
	require.NotNil(t, mockRegistry, "Resource registry should be in context")

	// Close the initial UOW before Execute - runner.Execute creates its own UOW
	// This prevents deadlock as coroutines would use the wrong UOW
	err = uw.Close()
	require.NoError(t, err)

	// Create a new context with resources but without UOW
	execCtx := resource.WithResources(context.Background(), mockRegistry)

	err = vm.Import(`
		function test_db_execute()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end
			local res, err = db:execute("UPDATE exec_test SET value = ? WHERE id = 1", {"new"})
			if err then error(err) end
			local rows, err = db:query("SELECT value FROM exec_test WHERE id = 1")
			if err then error(err) end
			return rows[1].value
		end
	`, "test", "test_db_execute")
	require.NoError(t, err)

	result, err := runner.Execute(execCtx, "test_db_execute")
	require.NoError(t, err)
	assert.Equal(t, lua.LString("new"), result)
}

// TestDBRelease verifies that db:release() returns success and that the resource is released.
func TestDBRelease(t *testing.T) {
	_, mockRes, cleanup := setupTestDBWithTestTable(t)
	defer cleanup()

	vm, uw, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() { _ = uw.Close() }()

	err := vm.Import(`
		function test_db_release()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end
			local ok, err = db:release()
			if err then error(err) end
			return ok
		end
	`, "test", "test_db_release")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_db_release")
	require.NoError(t, err)
	assert.Equal(t, lua.LTrue, result)
}

// TestDBGetNotFound verifies that requesting a non-existent resource returns an error.
func TestDBGetNotFound(t *testing.T) {
	_, mockRes, cleanup := setupTestDBWithTestTable(t)
	defer cleanup()

	vm, uw, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() { _ = uw.Close() }()

	err := vm.Import(`
		function test_db_get_not_found()
			local sql = require("sql")
			local db, err = sql.get("nonexistent_resource")
			if err then return err else return "no error" end
		end
	`, "test", "test_db_get_not_found")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_db_get_not_found")
	require.NoError(t, err)
	errMsg, ok := result.(lua.LString)
	require.True(t, ok)
	assert.Contains(t, string(errMsg), "failed to acquire resource")
}

// TestDBQueryNoResults verifies that a query returning no rows is handled correctly.
func TestDBQueryNoResults(t *testing.T) {
	_, mockRes, cleanup := setupTestDBWithTestTable(t)
	defer cleanup()

	vm, uw, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() { _ = uw.Close() }()

	err := vm.Import(`
		function test_db_query_no_results()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end
			local rows, err = db:query("SELECT value FROM test WHERE id > 100")
			if err then error(err) end
			return { count = #rows }
		end
	`, "test", "test_db_query_no_results")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_db_query_no_results")
	require.NoError(t, err)
	resultTable, ok := result.(*lua.LTable)
	require.True(t, ok)
	count := resultTable.RawGetString("count")
	assert.Equal(t, lua.LNumber(0), count)
}

// TestDBErrorHandling verifies that an invalid query returns an appropriate error.
func TestDBErrorHandling(t *testing.T) {
	_, mockRes, cleanup := setupTestDBWithTestTable(t)
	defer cleanup()

	vm, uw, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() { _ = uw.Close() }()

	err := vm.Import(`
		function test_db_error()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end
			local rows, err = db:query("SELECT non_existing_column FROM test")
			if err then return err end
			return "no error"
		end
	`, "test", "test_db_error")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_db_error")
	require.NoError(t, err)
	errMsg, ok := result.(lua.LString)
	require.True(t, ok)
	assert.Contains(t, string(errMsg), "no such column")
}
