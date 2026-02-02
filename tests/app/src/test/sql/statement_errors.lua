-- Test: sql prepared statement error handling
local assert = require("assert_primitives")

local function main()
	local sql = require("sql")

	local db, err = sql.get("app.test.sql:testdb")
	assert.is_nil(err, "should get database")

	-- Create test table
	db:execute("CREATE TABLE IF NOT EXISTS stmt_err_test (id INTEGER PRIMARY KEY, name TEXT, value INTEGER)")
	db:execute("DELETE FROM stmt_err_test")

	-- Test operations on closed statement
	local stmt, err1 = db:prepare("SELECT * FROM stmt_err_test WHERE id = ?")
	assert.is_nil(err1, "prepare should not error")

	-- Close the statement
	local ok, err2 = stmt:close()
	assert.is_nil(err2, "close should not error")
	assert.eq(ok, true, "close should return true")

	-- Query on closed statement should error
	local _, err3 = stmt:query({1})
	assert.not_nil(err3, "query on closed stmt should error")
	assert.eq(err3:kind(), errors.INVALID, "closed stmt query error kind")
	assert.eq(err3:retryable(), false, "closed stmt not retryable")

	-- Execute on closed statement should error
	local _, err4 = stmt:execute({1})
	assert.not_nil(err4, "execute on closed stmt should error")
	assert.eq(err4:kind(), errors.INVALID, "closed stmt execute error kind")

	-- Double close should error
	local _, err5 = stmt:close()
	assert.not_nil(err5, "double close should error")
	assert.eq(err5:kind(), errors.INVALID, "double close error kind")

	-- Test statement with NULL parameters
	local stmt2, _ = db:prepare("INSERT INTO stmt_err_test (name, value) VALUES (?, ?)")

	-- Insert with NULL using sql.NULL
	local res1, err6 = stmt2:execute({"test_null", sql.NULL})
	assert.is_nil(err6, "insert with sql.NULL should not error")
	assert.eq(res1.rows_affected, 1, "should insert 1 row")

	-- Insert with sql.as.null()
	local res2, err7 = stmt2:execute({sql.as.null(), 123})
	assert.is_nil(err7, "insert with as.null should not error")

	stmt2:close()

	-- Verify NULL values
	local rows, _ = db:query("SELECT name, value FROM stmt_err_test ORDER BY id")
	assert.eq(#rows, 2, "should have 2 rows")
	assert.eq(rows[1].name, "test_null", "first name should be test_null")
	assert.is_nil(rows[1].value, "first value should be nil")
	assert.is_nil(rows[2].name, "second name should be nil")
	assert.eq(rows[2].value, 123, "second value should be 123")

	-- Test statement query with no results
	local stmt3, _ = db:prepare("SELECT * FROM stmt_err_test WHERE id = ?")
	local rows2, err8 = stmt3:query({99999})
	assert.is_nil(err8, "query no results should not error")
	assert.eq(#rows2, 0, "should have 0 rows")
	stmt3:close()

	-- Test statement with various data types
	local stmt4, _ = db:prepare("INSERT INTO stmt_err_test (name, value) VALUES (?, ?)")

	-- Integer
	stmt4:execute({"int_test", 42})

	-- Float (will be stored as integer in SQLite)
	stmt4:execute({"float_test", sql.as.int(99)})

	-- Typed values
	stmt4:execute({sql.as.text("typed_text"), sql.as.int(100)})

	stmt4:close()

	-- Verify data types
	local rows3, _ = db:query("SELECT name, value FROM stmt_err_test WHERE name = ?", {"int_test"})
	assert.eq(rows3[1].value, 42, "int value should be 42")
	assert.eq(math.type(rows3[1].value), "integer", "should be integer type")

	-- Test statement reuse (multiple executions)
	local stmt5, _ = db:prepare("SELECT COUNT(*) as cnt FROM stmt_err_test WHERE value > ?")

	local r1, _ = stmt5:query({0})
	local r2, _ = stmt5:query({50})
	local r3, _ = stmt5:query({100})

	assert.eq(r1[1].cnt > r2[1].cnt, true, "cnt > 0 should be more than cnt > 50")
	assert.eq(r2[1].cnt >= r3[1].cnt, true, "cnt > 50 should be >= cnt > 100")

	stmt5:close()

	-- Test prepared statement in transaction
	local tx, _ = db:begin()
	local tx_stmt, err9 = tx:prepare("INSERT INTO stmt_err_test (name, value) VALUES (?, ?)")
	assert.is_nil(err9, "prepare in tx should not error")

	tx_stmt:execute({"tx_item1", 1})
	tx_stmt:execute({"tx_item2", 2})
	tx_stmt:close()

	tx:commit()

	-- Verify tx inserts
	local rows4, _ = db:query("SELECT COUNT(*) as cnt FROM stmt_err_test WHERE name LIKE 'tx_%'")
	assert.eq(rows4[1].cnt, 2, "should have 2 tx items")

	-- Cleanup
	db:execute("DROP TABLE stmt_err_test")
	db:release()

	return true
end

return { main = main }
