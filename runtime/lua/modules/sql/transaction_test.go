package sql

import (
	"database/sql"
	"testing"

	sqlres "github.com/wippyai/runtime/service/sql"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

// TestTransactionCommit verifies that a transaction commit persists changes.
func TestTransactionCommit(t *testing.T) {
	// Open an in-memory SQLite database.
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { assert.NoError(t, db.Close()) }()

	// Create a table and insert an initial row.
	_, err = db.ExecContext(t.Context(), "CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "INSERT INTO items (value) VALUES ('old')")
	require.NoError(t, err)

	// Wrap the DB in a mockResource for Lua integration.
	mockRes := &mockResource{
		resValue: sqlres.DBResource{
			DB:   db,
			Type: "sqlite",
		},
	}

	// Setup the Lua VM with the SQL module (helper defined in module_test.go).
	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Imports a Lua script that begins a transaction, updates a row, commits, and queries the change.
	script := `
		function test_tx_commit()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end
			-- Begin transaction
			local tx, err = db:begin()
			if err then error(err) end
			-- Update the value inside the transaction
			local res, err = tx:execute("UPDATE items SET value = ? WHERE id = 1", {"new"})
			if err then error(err) end
			-- Commit the transaction
			local ok, err = tx:commit()
			if err then error(err) end
			-- Query the updated value
			local rows, err = db:query("SELECT value FROM items WHERE id = 1")
			if err then error(err) end
			return rows[1].value
		end
	`
	err = vm.Import(script, "test", "test_tx_commit")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_tx_commit")
	require.NoError(t, err)
	assert.Equal(t, lua.LString("new"), result)
}

// TestTransactionRollback verifies that rolling back a transaction leaves data unchanged.
func TestTransactionRollback(t *testing.T) {
	// Open an in-memory SQLite database.
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { assert.NoError(t, db.Close()) }()

	// Create table and initial data.
	_, err = db.ExecContext(t.Context(), "CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "INSERT INTO items (value) VALUES ('old')")
	require.NoError(t, err)

	mockRes := &mockResource{
		resValue: sqlres.DBResource{
			DB:   db,
			Type: "sqlite",
		},
	}

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Lua script: update inside a transaction then rollback.
	script := `
		function test_tx_rollback()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end
			local tx, err = db:begin()
			if err then error(err) end
			local res, err = tx:execute("UPDATE items SET value = ? WHERE id = 1", {"new"})
			if err then error(err) end
			local ok, err = tx:rollback()
			if err then error(err) end
			local rows, err = db:query("SELECT value FROM items WHERE id = 1")
			if err then error(err) end
			return rows[1].value
		end
	`
	err = vm.Import(script, "test", "test_tx_rollback")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_tx_rollback")
	require.NoError(t, err)
	// Expect the original value since the update was rolled back.
	assert.Equal(t, lua.LString("old"), result)
}

// TestTransactionSavepoint verifies rollback to a savepoint.
func TestTransactionSavepoint(t *testing.T) {
	// Open an in-memory SQLite database.
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { assert.NoError(t, db.Close()) }()

	// Create table and insert initial data.
	_, err = db.ExecContext(t.Context(), "CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "INSERT INTO items (value) VALUES ('old')")
	require.NoError(t, err)

	mockRes := &mockResource{
		resValue: sqlres.DBResource{
			DB:   db,
			Type: "sqlite",
		},
	}

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Lua script: update row, create a savepoint, perform a further update, rollback to the savepoint, then commit.
	script := `
		function test_tx_savepoint()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end
			local tx, err = db:begin()
			if err then error(err) end
			-- First update: set value to "intermediate"
			local res, err = tx:execute("UPDATE items SET value = ? WHERE id = 1", {"intermediate"})
			if err then error(err) end
			-- Create savepoint
			local sp, err = tx:savepoint("sp1")
			if err then error(err) end
			-- Second update: change value to "final"
			res, err = tx:execute("UPDATE items SET value = ? WHERE id = 1", {"final"})
			if err then error(err) end
			-- Rollback to savepoint, reverting the second update
			local ok, err = tx:rollback_to("sp1")
			if err then error(err) end
			-- Commit the transaction
			ok, err = tx:commit()
			if err then error(err) end
			-- Query the final value
			local rows, err = db:query("SELECT value FROM items WHERE id = 1")
			if err then error(err) end
			return rows[1].value
		end
	`
	err = vm.Import(script, "test", "test_tx_savepoint")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_tx_savepoint")
	require.NoError(t, err)
	// Expect the value to remain as "intermediate" (the change to "final" was undone).
	assert.Equal(t, lua.LString("intermediate"), result)
}

func TestTransactionErrorHandling(t *testing.T) {
	// Open an in-memory SQLite database.
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { assert.NoError(t, db.Close()) }()

	// Create a simple table and insert initial data.
	_, err = db.ExecContext(t.Context(), "CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)")
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "INSERT INTO items (value) VALUES ('old')")
	require.NoError(t, err)

	// Wrap the DB in a mockResource for Lua integration.
	mockRes := &mockResource{
		resValue: sqlres.DBResource{
			DB:   db,
			Type: "sqlite",
		},
	}

	// Set up the Lua VM with the SQL module.
	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Lua script that begins a transaction, performs a valid update,
	// then executes an invalid query to trigger an error and rollback.
	script := `
		function test_tx_error()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end
			-- Begin transaction
			local tx, err = db:begin()
			if err then error(err) end

			-- Valid update
			local res, err = tx:execute("UPDATE items SET value = 'new' WHERE id = 1")
			if err then error(err) end

			-- This update targets a non-existent table, should error.
			local res2, err = tx:execute("UPDATE non_existing_table SET value = 'fail'")
			if err then 
				-- Rollback on error and return the error message.
				tx:rollback()
				return err
			else
				-- If no error occurs (unexpected), commit the transaction.
				local ok, err = tx:commit()
				if err then error(err) end
				return "no error"
			end
		end
	`
	err = vm.Import(script, "test", "test_tx_error")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_tx_error")
	require.NoError(t, err)

	// The result should contain an error message mentioning the non_existing_table.
	assert.Contains(t, result.String(), "non_existing_table")
}
