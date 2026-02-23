-- SPDX-License-Identifier: MPL-2.0

-- Test: sql prepared statement operations
local assert = require("assert_primitives")

local function main()
	local sql = require("sql")

	local db, err = sql.get("app.test.sql:testdb")
	assert.is_nil(err, "should get database")

	-- Create test table
	local _, create_err = db:execute("CREATE TABLE IF NOT EXISTS stmt_test (id INTEGER PRIMARY KEY, name TEXT, value INTEGER)")
	assert.is_nil(create_err, "should create table")

	local _, del_err = db:execute("DELETE FROM stmt_test")
	assert.is_nil(del_err, "should delete from table")

	-- Prepare insert statement
	local insert_stmt, err1 = db:prepare("INSERT INTO stmt_test (name, value) VALUES (?, ?)")
	assert.is_nil(err1, "prepare insert should not error")
	assert.not_nil(insert_stmt, "should have statement")

	-- Execute prepared statement multiple times
	local _, err2 = insert_stmt:execute({"item1", 100})
	assert.is_nil(err2, "first execute should not error")

	local _, err3 = insert_stmt:execute({"item2", 200})
	assert.is_nil(err3, "second execute should not error")

	local _, err4 = insert_stmt:execute({"item3", 300})
	assert.is_nil(err4, "third execute should not error")

	-- Close insert statement
	local ok1, err5 = insert_stmt:close()
	assert.is_nil(err5, "close should not error")
	assert.eq(ok1, true, "close should return true")

	-- Prepare query statement
	local query_stmt, err6 = db:prepare("SELECT name, value FROM stmt_test WHERE value > ?")
	assert.is_nil(err6, "prepare query should not error")

	-- Query with prepared statement
	local rows1, err7 = query_stmt:query({150})
	assert.is_nil(err7, "prepared query should not error")
	assert.eq(#rows1, 2, "should find 2 rows > 150")

	local rows2, err8 = query_stmt:query({250})
	assert.is_nil(err8, "second prepared query should not error")
	assert.eq(#rows2, 1, "should find 1 row > 250")
	assert.eq(rows2[1].name, "item3", "should find item3")

	query_stmt:close()

	-- Test prepared statement in transaction
	local tx, err9 = db:begin()
	assert.is_nil(err9, "begin should not error")

	local tx_stmt, err10 = tx:prepare("UPDATE stmt_test SET value = value + ? WHERE name = ?")
	assert.is_nil(err10, "prepare in tx should not error")

	tx_stmt:execute({50, "item1"})
	tx_stmt:close()

	tx:commit()

	-- Verify update
	local rows3, _ = db:query("SELECT value FROM stmt_test WHERE name = ?", {"item1"})
	local row3 = rows3[1]
	assert.not_nil(row3, "should have row")
	assert.eq(row3.value, 150, "value should be updated to 150")

	-- Cleanup
	db:execute("DROP TABLE stmt_test")
	db:release()

	return true
end

return { main = main }
