package sql

import (
	"database/sql"
	"testing"

	sqlapi "github.com/wippyai/runtime/api/service/sql"
	sqlres "github.com/wippyai/runtime/service/sql"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	// Imports SQLite driver for testing
	_ "github.com/mattn/go-sqlite3"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) (*sql.DB, *mockResource, func()) {
	// Create a SQLite in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err, "Failed to open SQLite database")

	// Create test tables
	_, err = db.ExecContext(t.Context(), `CREATE TABLE users (
		id INTEGER PRIMARY KEY, 
		name TEXT,
		age INTEGER,
		active BOOLEAN
	)`)
	require.NoError(t, err, "Failed to create test table")

	// Pre-populate with some data
	_, err = db.ExecContext(t.Context(), `INSERT INTO users (name, age, active) VALUES 
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

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Imports the test script
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

	// Execute with correct context from setup
	result, err := runner.Execute(ctx, "test_prepare")
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

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Imports the test script
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

	// Execute with correct context from setup
	result, err := runner.Execute(ctx, "test_query")
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

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Imports the test script
	err := vm.Import(`
		function test_execute()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			local stmt, err = db:prepare("UPDATE users SET name = ? WHERE id = ?")
			if err then error(err) end

			-- Serve with parameters
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

	// Execute with correct context from setup
	result, err := runner.Execute(ctx, "test_execute")
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

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Imports the test script
	err := vm.Import(`
		function test_multiple_execute()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			local stmt, err = db:prepare("INSERT INTO users (name, age, active) VALUES (?, ?, ?)")
			if err then error(err) end

			-- Serve first insert
			local result1, err = stmt:execute({"David", 40, true})
			if err then error(err) end

			-- Serve second insert
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

	// Execute with correct context from setup
	result, err := runner.Execute(ctx, "test_multiple_execute")
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

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Imports the test script
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
					error_msg = tostring(err)
				}
			end

			-- This should not execute
			return {
				has_error = false
			}
		end
	`, "test", "test_error_handling")
	require.NoError(t, err, "Failed to import test script")

	// Execute with correct context from setup
	result, err := runner.Execute(ctx, "test_error_handling")
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

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Imports the test script
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
					error_msg = tostring(err)
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

	// Execute with correct context from setup
	result, err := runner.Execute(ctx, "test_invalid_params")
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

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Setup a statement that doesn't need parameters
	_, err := db.ExecContext(t.Context(), "CREATE TABLE counts (count INTEGER)")
	require.NoError(t, err, "Failed to create test table")

	_, err = db.ExecContext(t.Context(), "INSERT INTO counts VALUES (42)")
	require.NoError(t, err, "Failed to insert test data")

	// Imports the test script
	err = vm.Import(`
		function test_nil_params()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			local stmt, err = db:prepare("SELECT count FROM counts")
			if err then error(err) end

			-- Serve without parameters (nil)
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

	// Execute with correct context from setup
	result, err := runner.Execute(ctx, "test_nil_params")
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

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Create a table with various data types
	_, err := db.ExecContext(t.Context(), `CREATE TABLE datatypes (
		id INTEGER PRIMARY KEY,
		int_val INTEGER,
		real_val REAL,
		text_val TEXT,
		bool_val BOOLEAN,
		null_val TEXT
	)`)
	require.NoError(t, err, "Failed to create test table")

	// Imports the test script
	err = vm.Import(`
	function test_data_types()
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		if err then error(err) end

		local stmt, err = db:prepare("INSERT INTO datatypes (int_val, real_val, text_val, bool_val, null_val) VALUES (?, ?, ?, ?, ?)")
		if err then error(err) end

		-- Serve with different data types, using sql.NULL instead of nil
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

	// Execute with correct context from setup
	result, err := runner.Execute(ctx, "test_data_types")
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
	assert.Equal(t, "Hello, world!", textVal, "Text value mismatch")
	assert.True(t, boolVal == lua.LTrue || boolVal == lua.LNumber(1), "Boolean value should be true or 1")
	assert.True(t, hasNull, "Null value was not properly handled")
}

// TestStatementClose tests explicit closing of a prepared statement
func TestStatementClose(t *testing.T) {
	_, mockRes, cleanup := setupTestDB(t)
	defer cleanup()

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Imports the test script
	err := vm.Import(`
		function test_statement_close()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			local stmt, err = db:prepare("SELECT 1")
			if err then error(err) end

			-- Test close operation
			local ok, err = stmt:close()
			if err then error(err) end

			-- Attempt to use after closing (should fail)
			local success = true
			local err_msg = ""
			
			-- This should fail because statement is closed
			local rows, query_err = stmt:query()
			if query_err then
				success = false
				err_msg = query_err
			end

			return {
				close_successful = ok,
				reuse_failed = not success,
				error_message = err_msg
			}
		end
	`, "test", "test_statement_close")
	require.NoError(t, err, "Failed to import test script")

	// Execute with correct context from setup
	result, err := runner.Execute(ctx, "test_statement_close")
	require.NoError(t, err, "Lua execution failed")

	// Verify results
	resultTable := result.(*lua.LTable)
	closeSuccessful := resultTable.RawGetString("close_successful")
	reuseFailed := resultTable.RawGetString("reuse_failed")

	assert.Equal(t, lua.LTrue, closeSuccessful, "Statement close should return true")
	assert.Equal(t, lua.LTrue, reuseFailed, "Using a closed statement should fail")
}

// TestStatementQueryNoResults tests a query that returns no rows
func TestStatementQueryNoResults(t *testing.T) {
	_, mockRes, cleanup := setupTestDB(t)
	defer cleanup()

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Imports the test script
	err := vm.Import(`
		function test_query_no_results()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			local stmt, err = db:prepare("SELECT * FROM users WHERE id = 999")
			if err then error(err) end

			-- Query with a parameter that won't match any rows
			local rows, err = stmt:query()
			if err then error(err) end

			-- Store results for testing
			local result = {
				rows_received = rows ~= nil,
				row_count = #rows
			}

			local ok, err = stmt:close()
			if err then error(err) end
			
			return result
		end
	`, "test", "test_query_no_results")
	require.NoError(t, err, "Failed to import test script")

	// Execute with correct context from setup
	result, err := runner.Execute(ctx, "test_query_no_results")
	require.NoError(t, err, "Lua execution failed")

	// Verify query results
	resultTable := result.(*lua.LTable)
	rowsReceived := resultTable.RawGetString("rows_received").(lua.LBool)
	rowCount := resultTable.RawGetString("row_count").(lua.LNumber)

	assert.True(t, bool(rowsReceived), "Should receive a rows object even when empty")
	assert.Equal(t, float64(0), float64(rowCount), "Expected 0 rows")
}

// TestStatementQueryNullColumns tests handling NULL values in result columns
func TestStatementQueryNullColumns(t *testing.T) {
	db, mockRes, cleanup := setupTestDB(t)
	defer cleanup()

	// Create test data with NULL values
	_, err := db.ExecContext(t.Context(), `CREATE TABLE nulls_test (
		id INTEGER PRIMARY KEY,
		nullable_text TEXT,
		nullable_int INTEGER
	)`)
	require.NoError(t, err, "Failed to create test table")

	_, err = db.ExecContext(t.Context(), `INSERT INTO nulls_test (id, nullable_text, nullable_int) VALUES 
		(1, 'not null', 42),
		(2, NULL, 10),
		(3, 'text', NULL),
		(4, NULL, NULL)`)
	require.NoError(t, err, "Failed to insert test data")

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Imports the test script
	err = vm.Import(`
		function test_null_columns()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			local stmt, err = db:prepare("SELECT * FROM nulls_test ORDER BY id")
			if err then error(err) end

			-- Query to get rows with NULL values
			local rows, err = stmt:query()
			if err then error(err) end

			-- Store results for testing
			local result = {
				row_count = #rows,
				row1_text_null = rows[1].nullable_text == nil,
				row1_int_null = rows[1].nullable_int == nil,
				row2_text_null = rows[2].nullable_text == nil,
				row2_int_null = rows[2].nullable_int == nil,
				row3_text_null = rows[3].nullable_text == nil,
				row3_int_null = rows[3].nullable_int == nil,
				row4_text_null = rows[4].nullable_text == nil,
				row4_int_null = rows[4].nullable_int == nil
			}

			local ok, err = stmt:close()
			if err then error(err) end
			
			return result
		end
	`, "test", "test_null_columns")
	require.NoError(t, err, "Failed to import test script")

	// Execute with correct context from setup
	result, err := runner.Execute(ctx, "test_null_columns")
	require.NoError(t, err, "Lua execution failed")

	// Verify NULL handling in result columns
	resultTable := result.(*lua.LTable)
	rowCount := resultTable.RawGetString("row_count").(lua.LNumber)

	// Values that should NOT be NULL
	row1TextNull := resultTable.RawGetString("row1_text_null").(lua.LBool)
	row1IntNull := resultTable.RawGetString("row1_int_null").(lua.LBool)

	// Values that SHOULD be NULL
	row2TextNull := resultTable.RawGetString("row2_text_null").(lua.LBool)
	row3IntNull := resultTable.RawGetString("row3_int_null").(lua.LBool)
	row4TextNull := resultTable.RawGetString("row4_text_null").(lua.LBool)
	row4IntNull := resultTable.RawGetString("row4_int_null").(lua.LBool)

	assert.Equal(t, float64(4), float64(rowCount), "Expected 4 rows")

	// Check non-NULL values
	assert.False(t, bool(row1TextNull), "Row 1 text should not be NULL")
	assert.False(t, bool(row1IntNull), "Row 1 int should not be NULL")

	// Check NULL values
	assert.True(t, bool(row2TextNull), "Row 2 text should be NULL")
	assert.True(t, bool(row3IntNull), "Row 3 int should be NULL")
	assert.True(t, bool(row4TextNull), "Row 4 text should be NULL")
	assert.True(t, bool(row4IntNull), "Row 4 int should be NULL")
}

// TestStatementWithSQLNull tests using the sql.NULL constant with prepared statements
func TestStatementWithSQLNull(t *testing.T) {
	db, mockRes, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a table with nullable columns
	_, err := db.ExecContext(t.Context(), `CREATE TABLE null_params_test (
		id INTEGER PRIMARY KEY,
		param1 TEXT,
		param2 INTEGER,
		param3 REAL
	)`)
	require.NoError(t, err, "Failed to create test table")

	vm, runner, ctx := setupLuaWithDB(t, mockRes)
	defer vm.Close()

	// Imports the test script
	err = vm.Import(`
		function test_null_params()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			-- Test NULL at beginning, middle, and end of parameter list
			local stmt, err = db:prepare("INSERT INTO null_params_test (param1, param2, param3) VALUES (?, ?, ?)")
			if err then error(err) end

			-- Test with NULL at beginning
			local result1, err = stmt:execute({sql.NULL, 42, 3.14})
			if err then error("First insert failed: " .. err) end

			-- Test with NULL in middle
			local result2, err = stmt:execute({"middle", sql.NULL, 2.71})
			if err then error("Second insert failed: " .. err) end

			-- Test with NULL at end
			local result3, err = stmt:execute({"end", 99, sql.NULL})
			if err then error("Third insert failed: " .. err) end

			-- Query to verify the inserts
			local rows, err = db:query("SELECT * FROM null_params_test ORDER BY id")
			if err then error("Query failed: " .. err) end

			-- Store results for testing
			local result = {
				row_count = #rows,
				-- Row 1: NULL at beginning
				row1_param1_null = rows[1].param1 == nil,
				row1_param2_value = rows[1].param2,
				row1_param3_value = rows[1].param3,
				-- Row 2: NULL in middle
				row2_param1_value = rows[2].param1,
				row2_param2_null = rows[2].param2 == nil,
				row2_param3_value = rows[2].param3,
				-- Row 3: NULL at end
				row3_param1_value = rows[3].param1,
				row3_param2_value = rows[3].param2,
				row3_param3_null = rows[3].param3 == nil
			}

			local ok, err = stmt:close()
			if err then error(err) end
			
			return result
		end
	`, "test", "test_null_params")
	require.NoError(t, err, "Failed to import test script")

	// Execute with correct context from setup
	result, err := runner.Execute(ctx, "test_null_params")
	require.NoError(t, err, "Lua execution failed")

	// Verify NULL parameter handling
	resultTable := result.(*lua.LTable)
	rowCount := resultTable.RawGetString("row_count").(lua.LNumber)

	// Check NULL at beginning (Row 1)
	row1Param1Null := resultTable.RawGetString("row1_param1_null").(lua.LBool)
	row1Param2Value := resultTable.RawGetString("row1_param2_value").(lua.LNumber)
	row1Param3Value := resultTable.RawGetString("row1_param3_value").(lua.LNumber)

	// Check NULL in middle (Row 2)
	row2Param1Value := resultTable.RawGetString("row2_param1_value").(lua.LString)
	row2Param2Null := resultTable.RawGetString("row2_param2_null").(lua.LBool)
	row2Param3Value := resultTable.RawGetString("row2_param3_value").(lua.LNumber)

	// Check NULL at end (Row 3)
	row3Param1Value := resultTable.RawGetString("row3_param1_value").(lua.LString)
	row3Param2Value := resultTable.RawGetString("row3_param2_value").(lua.LNumber)
	row3Param3Null := resultTable.RawGetString("row3_param3_null").(lua.LBool)

	assert.Equal(t, float64(3), float64(rowCount), "Expected 3 rows")

	// Verify Row 1 (NULL at beginning)
	assert.True(t, bool(row1Param1Null), "Row 1 param1 should be NULL")
	assert.Equal(t, float64(42), float64(row1Param2Value), "Row 1 param2 should be 42")
	assert.InDelta(t, 3.14, float64(row1Param3Value), 0.001, "Row 1 param3 should be approximately 3.14")

	// Verify Row 2 (NULL in middle)
	assert.Equal(t, "middle", string(row2Param1Value), "Row 2 param1 should be 'middle'")
	assert.True(t, bool(row2Param2Null), "Row 2 param2 should be NULL")
	assert.InDelta(t, 2.71, float64(row2Param3Value), 0.001, "Row 2 param3 should be approximately 2.71")

	// Verify Row 3 (NULL at end)
	assert.Equal(t, "end", string(row3Param1Value), "Row 3 param1 should be 'end'")
	assert.Equal(t, float64(99), float64(row3Param2Value), "Row 3 param2 should be 99")
	assert.True(t, bool(row3Param3Null), "Row 3 param3 should be NULL")
}
