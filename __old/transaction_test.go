package __ignore

import (
	"github.com/ponyruntime/pony/runtime/lua/modules/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	// Import SQLite driver for testing
	_ "github.com/mattn/go-sqlite3"
)

// TestTransactionBasic tests basic transaction operations
func TestTransactionBasic(t *testing.T) {
	db, mockRes, cleanup := sql.setupTestDB(t)
	defer cleanup()

	L, uw := sql.setupLuaWithDB(t, mockRes)
	defer L.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Create test data
	_, err := db.Exec(`CREATE TABLE accounts (id INTEGER PRIMARY KEY, balance INTEGER)`)
	require.NoError(t, err, "Failed to create test table")

	_, err = db.Exec(`INSERT INTO accounts (id, balance) VALUES (1, 1000), (2, 2000)`)
	require.NoError(t, err, "Failed to insert test data")

	// Test transaction with commit
	err = L.DoString(`
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		if err then error(err) end

		-- Begin transaction
		local tx, err = db:begin()
		if err then error(err) end

		-- Update first account
		local result1, err = tx:execute("UPDATE accounts SET balance = balance - 500 WHERE id = 1")
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Update second account
		local result2, err = tx:execute("UPDATE accounts SET balance = balance + 500 WHERE id = 2")
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Commit transaction
		local ok, err = tx:commit()
		if err then error(err) end

		-- Query to verify changes
		local rows, err = db:query("SELECT id, balance FROM accounts ORDER BY id")
		if err then error(err) end

		-- Store results for testing
		test_result = {
			rows_affected1 = result1.rows_affected,
			rows_affected2 = result2.rows_affected,
			balance1 = rows[1].balance,
			balance2 = rows[2].balance
		}
	`)

	// Check for Lua errors
	require.NoError(t, err, "Lua execution failed")

	// Verify transaction results
	resultTable := L.GetGlobal("test_result").(*lua.LTable)
	balance1 := resultTable.RawGetString("balance1").(lua.LNumber)
	balance2 := resultTable.RawGetString("balance2").(lua.LNumber)

	assert.Equal(t, float64(500), float64(balance1), "First account balance should be 500")
	assert.Equal(t, float64(2500), float64(balance2), "Second account balance should be 2500")
}

// TestTransactionRollback tests transaction rollback
func TestTransactionRollback(t *testing.T) {
	db, mockRes, cleanup := sql.setupTestDB(t)
	defer cleanup()

	L, uw := sql.setupLuaWithDB(t, mockRes)
	defer L.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Create test data
	_, err := db.Exec(`CREATE TABLE accounts (id INTEGER PRIMARY KEY, balance INTEGER)`)
	require.NoError(t, err, "Failed to create test table")

	_, err = db.Exec(`INSERT INTO accounts (id, balance) VALUES (1, 1000), (2, 2000)`)
	require.NoError(t, err, "Failed to insert test data")

	// Test transaction with rollback
	err = L.DoString(`
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		if err then error(err) end

		-- Begin transaction
		local tx, err = db:begin()
		if err then error(err) end

		-- Update first account
		local result1, err = tx:execute("UPDATE accounts SET balance = balance - 500 WHERE id = 1")
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Update second account - intentional error with nonexistent id
		local result2, err = tx:execute("UPDATE accounts SET balance = balance + 500 WHERE id = 999")
		
		-- Store if we got an error
		test_result = {
			update_error = (err ~= nil)
		}

		-- Roll back transaction
		local ok, err = tx:rollback()
		if err then error(err) end

		-- Query to verify no changes were made
		local rows, err = db:query("SELECT id, balance FROM accounts ORDER BY id")
		if err then error(err) end

		-- Store results for testing
		test_result.balance1 = rows[1].balance
		test_result.balance2 = rows[2].balance
	`)

	// Check for Lua errors
	require.NoError(t, err, "Lua execution failed")

	// Verify transaction rollback
	resultTable := L.GetGlobal("test_result").(*lua.LTable)
	balance1 := resultTable.RawGetString("balance1").(lua.LNumber)
	balance2 := resultTable.RawGetString("balance2").(lua.LNumber)

	assert.Equal(t, float64(1000), float64(balance1), "First account balance should be unchanged")
	assert.Equal(t, float64(2000), float64(balance2), "Second account balance should be unchanged")
}

// TestTransactionPrepare tests preparing statements within a transaction
func TestTransactionPrepare(t *testing.T) {
	db, mockRes, cleanup := sql.setupTestDB(t)
	defer cleanup()

	L, uw := sql.setupLuaWithDB(t, mockRes)
	defer L.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Create test data
	_, err := db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY, message TEXT)`)
	require.NoError(t, err, "Failed to create test table")

	// Test preparing statements within a transaction
	err = L.DoString(`
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		if err then error(err) end

		-- Begin transaction
		local tx, err = db:begin()
		if err then error(err) end

		-- Prepare statement within transaction
		local stmt, err = tx:prepare("INSERT INTO logs (message) VALUES (?)")
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Execute prepared statement multiple times
		local result1, err = stmt:execute({"Transaction log entry 1"})
		if err then 
			tx:rollback()
			error(err) 
		end

		local result2, err = stmt:execute({"Transaction log entry 2"})
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Close the statement
		local ok, err = stmt:close()
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Commit transaction
		ok, err = tx:commit()
		if err then error(err) end

		-- Query to verify changes
		local rows, err = db:query("SELECT id, message FROM logs ORDER BY id")
		if err then error(err) end

		-- Store results for testing
		test_result = {
			row_count = #rows,
			message1 = rows[1].message,
			message2 = rows[2].message
		}
	`)

	// Check for Lua errors
	require.NoError(t, err, "Lua execution failed")

	// Verify transaction results
	resultTable := L.GetGlobal("test_result").(*lua.LTable)
	rowCount := resultTable.RawGetString("row_count").(lua.LNumber)
	message1 := resultTable.RawGetString("message1").(lua.LString)
	message2 := resultTable.RawGetString("message2").(lua.LString)

	assert.Equal(t, float64(2), float64(rowCount), "Expected 2 rows inserted")
	assert.Equal(t, "Transaction log entry 1", string(message1), "First message mismatch")
	assert.Equal(t, "Transaction log entry 2", string(message2), "Second message mismatch")
}

// TestTransactionQuery tests querying within a transaction
func TestTransactionQuery(t *testing.T) {
	db, mockRes, cleanup := sql.setupTestDB(t)
	defer cleanup()

	L, uw := sql.setupLuaWithDB(t, mockRes)
	defer L.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Create test data
	_, err := db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT, price REAL)`)
	require.NoError(t, err, "Failed to create test table")

	_, err = db.Exec(`INSERT INTO items (name, price) VALUES 
		('Item 1', 10.99), 
		('Item 2', 20.50), 
		('Item 3', 15.75)`)
	require.NoError(t, err, "Failed to insert test data")

	// Test querying within a transaction
	err = L.DoString(`
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		if err then error(err) end

		-- Begin transaction
		local tx, err = db:begin()
		if err then error(err) end

		-- Query in transaction
		local rows, err = tx:query("SELECT id, name, price FROM items WHERE price > ?", {15.00})
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Store initial results for testing
		test_result = {
			initial_count = #rows,
			items = {}
		}

		for i, row in ipairs(rows) do
			test_result.items[i] = {
				id = row.id,
				name = row.name,
				price = row.price
			}
		end

		-- Insert a new item in the transaction
		local insert_result, err = tx:execute("INSERT INTO items (name, price) VALUES (?, ?)", {"Item 4", 25.00})
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Query again to see the new item
		rows, err = tx:query("SELECT id, name, price FROM items WHERE price > ?", {15.00})
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Store updated results
		test_result.updated_count = #rows

		-- Commit transaction
		local ok, err = tx:commit()
		if err then error(err) end
	`)

	// Check for Lua errors
	require.NoError(t, err, "Lua execution failed")

	// Verify transaction query results
	resultTable := L.GetGlobal("test_result").(*lua.LTable)
	initialCount := resultTable.RawGetString("initial_count").(lua.LNumber)
	updatedCount := resultTable.RawGetString("updated_count").(lua.LNumber)

	assert.Equal(t, float64(2), float64(initialCount), "Initial count should be 2")
	assert.Equal(t, float64(3), float64(updatedCount), "Updated count should be 3")
}

// TestTransactionInactive tests handling of an inactive transaction
func TestTransactionInactive(t *testing.T) {
	db, mockRes, cleanup := sql.setupTestDB(t)
	defer cleanup()

	L, uw := sql.setupLuaWithDB(t, mockRes)
	defer L.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Test operations after transaction is committed or rolled back
	err := L.DoString(`
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		if err then error(err) end

		-- Begin transaction
		local tx, err = db:begin()
		if err then error(err) end

		-- Commit transaction
		local ok, err = tx:commit()
		if err then error(err) end

		-- Try to use the transaction after commit
		local result, err = tx:execute("SELECT 1")
		
		-- Store results for testing
		test_result = {
			has_error = (err ~= nil),
			error_msg = err
		}

		-- Try to commit again
		local ok, err = tx:commit()
		test_result.second_commit_error = (err ~= nil)
	`)

	// Check for Lua errors
	require.NoError(t, err, "Lua execution failed")

	// Verify inactive transaction handling
	resultTable := L.GetGlobal("test_result").(*lua.LTable)
	hasError := resultTable.RawGetString("has_error").(lua.LBool)
	secondCommitError := resultTable.RawGetString("second_commit_error").(lua.LBool)
	errorMsg := ""
	if errMsg := resultTable.RawGetString("error_msg"); errMsg != lua.LNil {
		errorMsg = string(errMsg.(lua.LString))
	}

	assert.True(t, bool(hasError), "Expected an error when using inactive transaction")
	assert.True(t, bool(secondCommitError), "Expected an error when committing twice")
	assert.Contains(t, errorMsg, "not active", "Error message should mention transaction not being active")
}

// TestTransactionSavepoint tests savepoint operations within a transaction
func TestTransactionSavepoint(t *testing.T) {
	db, mockRes, cleanup := sql.setupTestDB(t)
	defer cleanup()

	L, uw := sql.setupLuaWithDB(t, mockRes)
	defer L.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Create test data
	_, err := db.Exec(`CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT, stock INTEGER)`)
	require.NoError(t, err, "Failed to create test table")

	// Test savepoint operations
	err = L.DoString(`
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		if err then error(err) end

		-- Begin transaction
		local tx, err = db:begin()
		if err then error(err) end

		-- Insert initial product
		local result1, err = tx:execute("INSERT INTO products (name, stock) VALUES (?, ?)", {"Product A", 10})
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Create savepoint
		local ok, err = tx:savepoint("after_first_insert")
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Insert second product
		local result2, err = tx:execute("INSERT INTO products (name, stock) VALUES (?, ?)", {"Product B", 5})
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Query to see both products
		local rows, err = tx:query("SELECT id, name, stock FROM products ORDER BY id")
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Store results after second insert
		test_result = {
			count_after_second = #rows
		}

		-- Roll back to savepoint
		ok, err = tx:rollback_to("after_first_insert")
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Query again to see only the first product
		rows, err = tx:query("SELECT id, name, stock FROM products ORDER BY id")
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Store results after rollback to savepoint
		test_result.count_after_rollback = #rows
		test_result.product_name = rows[1].name

		-- Insert a different second product
		local result3, err = tx:execute("INSERT INTO products (name, stock) VALUES (?, ?)", {"Product C", 15})
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Commit transaction
		ok, err = tx:commit()
		if err then error(err) end

		-- Query final state outside transaction
		rows, err = db:query("SELECT id, name, stock FROM products ORDER BY id")
		if err then error(err) end

		-- Store final results
		test_result.final_count = #rows
		test_result.final_names = {}
		
		for i, row in ipairs(rows) do
			test_result.final_names[i] = row.name
		end
	`)

	// Check for Lua errors
	require.NoError(t, err, "Lua execution failed")

	// Verify savepoint operations
	resultTable := L.GetGlobal("test_result").(*lua.LTable)
	countAfterSecond := resultTable.RawGetString("count_after_second").(lua.LNumber)
	countAfterRollback := resultTable.RawGetString("count_after_rollback").(lua.LNumber)
	finalCount := resultTable.RawGetString("final_count").(lua.LNumber)

	assert.Equal(t, float64(2), float64(countAfterSecond), "Expected 2 products after second insert")
	assert.Equal(t, float64(1), float64(countAfterRollback), "Expected 1 product after rollback to savepoint")
	assert.Equal(t, float64(2), float64(finalCount), "Expected 2 products in final state")

	// Check final product names
	finalNames := make([]string, 0)
	namesTable := resultTable.RawGetString("final_names").(*lua.LTable)
	namesTable.ForEach(func(_ lua.LValue, v lua.LValue) {
		finalNames = append(finalNames, string(v.(lua.LString)))
	})

	assert.Equal(t, []string{"Product A", "Product C"}, finalNames,
		"Expected Product A and Product C in final state (Product B was rolled back)")
}

// TestTransactionReleaseSavepoint tests releasing savepoints
func TestTransactionReleaseSavepoint(t *testing.T) {
	db, mockRes, cleanup := sql.setupTestDB(t)
	defer cleanup()

	L, uw := sql.setupLuaWithDB(t, mockRes)
	defer L.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Test releasing savepoints
	err := L.DoString(`
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		if err then error(err) end

		-- Begin transaction
		local tx, err = db:begin()
		if err then error(err) end

		-- Create savepoint
		local ok, err = tx:savepoint("sp1")
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Release savepoint
		ok, err = tx:release("sp1")
		if err then 
			tx:rollback()
			error(err) 
		end

		-- Try to rollback to released savepoint
		ok, err = tx:rollback_to("sp1")
		
		-- Store results for testing
		test_result = {
			has_error = (err ~= nil),
			error_msg = err
		}

		-- Roll back entire transaction
		ok, err = tx:rollback()
		if err then error(err) end
	`)

	// Check for Lua errors
	require.NoError(t, err, "Lua execution failed")

	// Verify released savepoint behavior
	resultTable := L.GetGlobal("test_result").(*lua.LTable)
	hasError := resultTable.RawGetString("has_error").(lua.LBool)

	assert.True(t, bool(hasError), "Expected an error when rolling back to released savepoint")
}

// TestTransactionInvalidSavepointName tests savepoint name validation
func TestTransactionInvalidSavepointName(t *testing.T) {
	db, mockRes, cleanup := sql.setupTestDB(t)
	defer cleanup()

	L, uw := sql.setupLuaWithDB(t, mockRes)
	defer L.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Test invalid savepoint names
	err := L.DoString(`
		local sql = require("sql")
		local db, err = sql.get("app:test_db")
		if err then error(err) end

		-- Begin transaction
		local tx, err = db:begin()
		if err then error(err) end

		-- Try to create savepoint with SQL injection
		local ok, err = tx:savepoint("sp1; DROP TABLE users;")
		
		-- Store results for testing
		test_result = {
			has_error = (err ~= nil),
			error_msg = err
		}

		-- Roll back transaction
		ok, err = tx:rollback()
		if err then error(err) end
	`)

	// Check for Lua errors
	require.NoError(t, err, "Lua execution failed")

	// Verify savepoint name validation
	resultTable := L.GetGlobal("test_result").(*lua.LTable)
	hasError := resultTable.RawGetString("has_error").(lua.LBool)
	errorMsg := ""
	if errMsg := resultTable.RawGetString("error_msg"); errMsg != lua.LNil {
		errorMsg = string(errMsg.(lua.LString))
	}

	assert.True(t, bool(hasError), "Expected an error for invalid savepoint name")
	assert.Contains(t, errorMsg, "alphanumeric", "Error should mention alphanumeric restrictions")
}
