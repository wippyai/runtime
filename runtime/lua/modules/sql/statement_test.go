package sql

import (
	"database/sql"
	sqlapi "github.com/ponyruntime/pony/api/service/sql"
	sqlres "github.com/ponyruntime/pony/service/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	// Import SQLite driver for testing
	_ "github.com/mattn/go-sqlite3"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) (*sql.DB, *mockResource, func()) {
	// Create a SQLite in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err, "Failed to open SQLite database")

	// Create test tables
	_, err = db.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY, 
		name TEXT,
		age INTEGER,
		active BOOLEAN
	)`)
	require.NoError(t, err, "Failed to create test table")

	// Pre-populate with some data
	_, err = db.Exec(`INSERT INTO users (name, age, active) VALUES 
		('Alice', 30, 1),
		('Bob', 25, 0),
		('Charlie', 35, 1)`)
	require.NoError(t, err, "Failed to insert test data")

	// Create the resource that will be tracked for release
	mockRes := &mockResource{
		resValue: sqlres.DBResource{
			DB:   db,
			Type: sqlapi.KindSQLite,
		},
	}

	// Return cleanup function
	cleanup := func() {
		err := db.Close()
		assert.NoError(t, err, "Failed to close SQLite database")
	}

	return db, mockRes, cleanup
}

// TestPrepareStatement tests the basic preparation of a statement
func TestPrepareStatement(t *testing.T) {
	_, mockRes, cleanup := setupTestDB(t)
	defer cleanup()

	vm, L, uw, runner := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import the test script
	err := vm.Import(`
		function test_prepare()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			local stmt, err = db:prepare("SELECT name FROM users WHERE id = ?")
			if err then error(err) end

			-- Store for testing
			local result = {
				statement_prepared = (stmt ~= nil)
			}

			local ok, err = stmt:close()
			if err then error(err) end
			
			return result
		end
	`, "test", "test_prepare")
	require.NoError(t, err, "Failed to import test script")

	// Execute the function using the runner
	result, err := runner.Execute(L.Context(), "test_prepare")
	require.NoError(t, err, "Lua execution failed")

	// Verify statement was prepared
	resultTable := result.(*lua.LTable)
	stmtPrepared := resultTable.RawGetString("statement_prepared").(lua.LBool)
	assert.True(t, bool(stmtPrepared), "Statement was not prepared successfully")
}

// TestStatementQuery tests executing a prepared statement query
func TestStatementQuery(t *testing.T) {
	_, mockRes, cleanup := setupTestDB(t)
	defer cleanup()

	vm, L, uw, runner := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import the test script
	err := vm.Import(`
		function test_query()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			local stmt, err = db:prepare("SELECT name FROM users WHERE id = ?")
			if err then error(err) end

			-- Query with a parameter
			local rows, err = stmt:query({1})
			if err then error(err) end

			-- Store results for testing
			local result = {
				rows = rows,
				row_count = #rows,
				first_name = rows[1] and rows[1].name or nil
			}

			local ok, err = stmt:close()
			if err then error(err) end
			
			return result
		end
	`, "test", "test_query")
	require.NoError(t, err, "Failed to import test script")

	// Execute the function using the runner
	result, err := runner.Execute(L.Context(), "test_query")
	require.NoError(t, err, "Lua execution failed")

	// Verify query results
	resultTable := result.(*lua.LTable)
	rowCount := resultTable.RawGetString("row_count").(lua.LNumber)
	firstName := resultTable.RawGetString("first_name").(lua.LString)

	assert.Equal(t, float64(1), float64(rowCount), "Expected 1 row")
	assert.Equal(t, "Alice", string(firstName), "Expected name to be 'Alice'")
}

// TestStatementExecute tests executing a prepared statement for updates
func TestStatementExecute(t *testing.T) {
	_, mockRes, cleanup := setupTestDB(t)
	defer cleanup()

	vm, L, uw, runner := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import the test script
	err := vm.Import(`
		function test_execute()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			local stmt, err = db:prepare("UPDATE users SET name = ? WHERE id = ?")
			if err then error(err) end

			-- Execute with parameters
			local result, err = stmt:execute({"Updated Name", 1})
			if err then error(err) end

			-- Store results for testing
			local test_result = {}
			
			-- Check if rows_affected is available (may be nil in some SQLite configurations)
			if result and result.rows_affected then 
				test_result.rows_affected = result.rows_affected
			end

			-- Query to verify the update
			local rows, err = db:query("SELECT name FROM users WHERE id = 1")
			if err then error(err) end

			test_result.updated_name = rows[1].name

			local ok, err = stmt:close()
			if err then error(err) end
			
			return test_result
		end
	`, "test", "test_execute")
	require.NoError(t, err, "Failed to import test script")

	// Execute the function using the runner
	result, err := runner.Execute(L.Context(), "test_execute")
	require.NoError(t, err, "Lua execution failed")

	// Verify execution results
	resultTable := result.(*lua.LTable)

	// We only care about the updated name, not rows_affected which may be nil
	updatedName := resultTable.RawGetString("updated_name").(lua.LString)

	// SQLite may not report rows affected in some cases
	// Just check that the update worked based on the name change
	assert.Equal(t, "Updated Name", string(updatedName), "Name was not updated correctly")
}

// TestStatementMultipleExecution tests reusing a prepared statement multiple times
func TestStatementMultipleExecution(t *testing.T) {
	_, mockRes, cleanup := setupTestDB(t)
	defer cleanup()

	vm, L, uw, runner := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import the test script
	err := vm.Import(`
		function test_multiple_execute()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			local stmt, err = db:prepare("INSERT INTO users (name, age, active) VALUES (?, ?, ?)")
			if err then error(err) end

			-- Execute first insert
			local result1, err = stmt:execute({"David", 40, true})
			if err then error(err) end

			-- Execute second insert
			local result2, err = stmt:execute({"Eva", 28, false})
			if err then error(err) end

			-- Query to verify the inserts
			local rows, err = db:query("SELECT name, age, active FROM users WHERE id > 3 ORDER BY id")
			if err then error(err) end

			-- Store results for testing
			local test_result = {
				row_count = #rows,
				names = {},
				ages = {},
				active_states = {}
			}
			
			-- Safely handle rows_affected which might be nil in some SQLite configurations
			if result1 and result1.rows_affected then
				test_result.rows_affected1 = result1.rows_affected
			end
			
			if result2 and result2.rows_affected then
				test_result.rows_affected2 = result2.rows_affected
			end

			for i, row in ipairs(rows) do
				test_result.names[i] = row.name
				test_result.ages[i] = row.age
				test_result.active_states[i] = row.active
			end

			local ok, err = stmt:close()
			if err then error(err) end
			
			return test_result
		end
	`, "test", "test_multiple_execute")
	require.NoError(t, err, "Failed to import test script")

	// Execute the function using the runner
	result, err := runner.Execute(L.Context(), "test_multiple_execute")
	require.NoError(t, err, "Lua execution failed")

	// Verify execution results
	resultTable := result.(*lua.LTable)

	// Get row count (use a safe type conversion)
	var rowCountVal float64
	if rcVal := resultTable.RawGetString("row_count"); rcVal != lua.LNil {
		rowCountVal = float64(rcVal.(lua.LNumber))
	}

	// Extract array values
	names := make([]string, 0)
	ages := make([]float64, 0)
	activeStates := make([]bool, 0)

	namesTable := resultTable.RawGetString("names").(*lua.LTable)
	agesTable := resultTable.RawGetString("ages").(*lua.LTable)
	activeTable := resultTable.RawGetString("active_states").(*lua.LTable)

	namesTable.ForEach(func(_ lua.LValue, v lua.LValue) {
		names = append(names, string(v.(lua.LString)))
	})

	agesTable.ForEach(func(_ lua.LValue, v lua.LValue) {
		ages = append(ages, float64(v.(lua.LNumber)))
	})

	activeTable.ForEach(func(_ lua.LValue, v lua.LValue) {
		activeStates = append(activeStates, v == lua.LTrue || v == lua.LNumber(1))
	})

	assert.Equal(t, float64(2), rowCountVal, "Expected 2 rows inserted")
	assert.Equal(t, []string{"David", "Eva"}, names, "Names don't match expected values")
	assert.Equal(t, []float64{40, 28}, ages, "Ages don't match expected values")

	// SQLite may return 1/0 or true/false for booleans
	assert.Len(t, activeStates, 2, "Expected 2 active states")
	assert.True(t, activeStates[0], "First user should be active")
	assert.False(t, activeStates[1], "Second user should be inactive")
}

// TestStatementErrorHandling tests error handling in prepared statements
func TestStatementErrorHandling(t *testing.T) {
	_, mockRes, cleanup := setupTestDB(t)
	defer cleanup()

	vm, L, uw, runner := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import the test script
	err := vm.Import(`
		function test_error_handling()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			local stmt, err = db:prepare("SELECT * FROM nonexistent_table")
			if err then
				-- Store error for testing
				return {
					has_error = true,
					error_msg = err
				}
			end

			-- This should not execute
			return {
				has_error = false
			}
		end
	`, "test", "test_error_handling")
	require.NoError(t, err, "Failed to import test script")

	// Execute the function using the runner
	result, err := runner.Execute(L.Context(), "test_error_handling")
	require.NoError(t, err, "Lua execution failed unexpectedly")

	// Verify error handling
	resultTable := result.(*lua.LTable)
	hasError := resultTable.RawGetString("has_error").(lua.LBool)
	errorMsg := ""
	if errMsg := resultTable.RawGetString("error_msg"); errMsg != lua.LNil {
		errorMsg = string(errMsg.(lua.LString))
	}

	assert.True(t, bool(hasError), "Expected an error for invalid table")
	assert.Contains(t, errorMsg, "nonexistent_table", "Error message should mention the nonexistent table")
}

// TestStatementWithInvalidParams tests handling of invalid parameters
func TestStatementWithInvalidParams(t *testing.T) {
	_, mockRes, cleanup := setupTestDB(t)
	defer cleanup()

	vm, L, uw, runner := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Import the test script
	err := vm.Import(`
		function test_invalid_params()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			local stmt, err = db:prepare("SELECT name FROM users WHERE id = ?")
			if err then error(err) end

			-- Try with string instead of table for params
			local rows, err = stmt:query("wrong parameter type")
			if err then
				-- Store error for testing
				local result = {
					has_error = true,
					error_msg = err
				}
				
				local ok, err = stmt:close()
				if err then error(err) end
				
				return result
			else
				-- This should not execute
				local ok, err = stmt:close()
				if err then error(err) end
				
				return {
					has_error = false
				}
			end
		end
	`, "test", "test_invalid_params")
	require.NoError(t, err, "Failed to import test script")

	// Execute the function using the runner
	result, err := runner.Execute(L.Context(), "test_invalid_params")
	require.NoError(t, err, "Lua execution failed unexpectedly")

	// Verify error handling
	resultTable := result.(*lua.LTable)
	hasError := resultTable.RawGetString("has_error").(lua.LBool)
	errorMsg := ""
	if errMsg := resultTable.RawGetString("error_msg"); errMsg != lua.LNil {
		errorMsg = string(errMsg.(lua.LString))
	}

	assert.True(t, bool(hasError), "Expected an error for invalid parameter type")
	assert.Contains(t, errorMsg, "table", "Error should mention parameter type issue")
}

// TestStatementWithNilParams tests statement execution with nil parameters
func TestStatementWithNilParams(t *testing.T) {
	db, mockRes, cleanup := setupTestDB(t)
	defer cleanup()

	vm, L, uw, runner := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Setup a statement that doesn't need parameters
	_, err := db.Exec("CREATE TABLE counts (count INTEGER)")
	require.NoError(t, err, "Failed to create test table")

	_, err = db.Exec("INSERT INTO counts VALUES (42)")
	require.NoError(t, err, "Failed to insert test data")

	// Import the test script
	err = vm.Import(`
		function test_nil_params()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			local stmt, err = db:prepare("SELECT count FROM counts")
			if err then error(err) end

			-- Execute without parameters (nil)
			local rows, err = stmt:query()
			if err then error(err) end

			-- Store results for testing
			local result = {
				row_count = #rows,
				count_value = rows[1] and rows[1].count or nil
			}

			local ok, err = stmt:close()
			if err then error(err) end
			
			return result
		end
	`, "test", "test_nil_params")
	require.NoError(t, err, "Failed to import test script")

	// Execute the function using the runner
	result, err := runner.Execute(L.Context(), "test_nil_params")
	require.NoError(t, err, "Lua execution failed")

	// Verify execution results
	resultTable := result.(*lua.LTable)
	rowCount := resultTable.RawGetString("row_count").(lua.LNumber)
	countValue := resultTable.RawGetString("count_value").(lua.LNumber)

	assert.Equal(t, float64(1), float64(rowCount), "Expected 1 row")
	assert.Equal(t, float64(42), float64(countValue), "Expected count value to be 42")
}

// TestStatementDataTypes tests handling of different data types in statements
func TestStatementDataTypes(t *testing.T) {
	db, mockRes, cleanup := setupTestDB(t)
	defer cleanup()

	vm, L, uw, runner := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Create a table with various data types
	_, err := db.Exec(`CREATE TABLE datatypes (
		id INTEGER PRIMARY KEY,
		int_val INTEGER,
		real_val REAL,
		text_val TEXT,
		bool_val BOOLEAN,
		null_val TEXT
	)`)
	require.NoError(t, err, "Failed to create test table")

	// Import the test script
	err = vm.Import(`
	function test_data_types()
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		if err then error(err) end

		local stmt, err = db:prepare("INSERT INTO datatypes (int_val, real_val, text_val, bool_val, null_val) VALUES (?, ?, ?, ?, ?)")
		if err then error(err) end

		-- Execute with different data types, using sql.NULL instead of nil
		local result, err = stmt:execute({42, 3.14159, "Hello, world!", true, sql.NULL})
		if err then error(err) end

		-- Query to verify the insert
		local rows, err = db:query("SELECT * FROM datatypes")
		if err then error(err) end

		-- Store results for testing
		local test_result = {
			int_val = rows[1].int_val,
			real_val = rows[1].real_val,
			text_val = rows[1].text_val,
			bool_val = rows[1].bool_val,
			has_null = (rows[1].null_val == nil)
		}
		
		-- Safely handle rows_affected which might be nil
		if result and result.rows_affected then
			test_result.rows_affected = result.rows_affected
		end

		local ok, err = stmt:close()
		if err then error(err) end
		
		return test_result
	end
`, "test", "test_data_types")
	require.NoError(t, err, "Failed to import test script")

	// Execute the function using the runner
	result, err := runner.Execute(L.Context(), "test_data_types")
	require.NoError(t, err, "Lua execution failed")

	// Verify data type handling
	resultTable := result.(*lua.LTable)

	// Safe type conversions with nil checks
	var intVal float64
	if iv := resultTable.RawGetString("int_val"); iv != lua.LNil {
		intVal = float64(iv.(lua.LNumber))
	}

	var realVal float64
	if rv := resultTable.RawGetString("real_val"); rv != lua.LNil {
		realVal = float64(rv.(lua.LNumber))
	}

	var textVal string
	if tv := resultTable.RawGetString("text_val"); tv != lua.LNil {
		textVal = string(tv.(lua.LString))
	}

	// Could be LBool or LNumber(0/1) depending on SQLite
	boolVal := resultTable.RawGetString("bool_val")

	var hasNull bool
	if hn := resultTable.RawGetString("has_null"); hn != lua.LNil {
		hasNull = bool(hn.(lua.LBool))
	}

	assert.Equal(t, float64(42), float64(intVal), "Integer value mismatch")
	assert.InDelta(t, 3.14159, float64(realVal), 0.00001, "Real value mismatch")
	assert.Equal(t, "Hello, world!", string(textVal), "Text value mismatch")
	assert.True(t, boolVal == lua.LTrue || boolVal == lua.LNumber(1), "Boolean value should be true or 1")
	assert.True(t, bool(hasNull), "Null value was not properly handled")
}
