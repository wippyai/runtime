-- SPDX-License-Identifier: MPL-2.0

-- Test: sql builder insert operations
local assert = require("assert_primitives")

local function main()
	local sql = require("sql")

	local db, err = sql.get("app.test.sql:testdb")
	assert.is_nil(err, "should get database")

	-- Create test table
	local _, err1 = db:execute("CREATE TABLE IF NOT EXISTS builder_insert_test (id INTEGER PRIMARY KEY, name TEXT, value REAL, active INTEGER)")
	assert.is_nil(err1, "should create table")

	-- Test basic insert builder
	local insert = sql.builder.insert("builder_insert_test")
	assert.not_nil(insert, "should create insert builder")

	-- Test columns and values
	insert = insert:columns("name", "value", "active")
	:values("test1", 10.5, 1)

	-- Test to_sql
	local insert_sql, args = insert:to_sql()
	assert.not_nil(insert_sql, "should generate SQL")
	assert.contains(insert_sql, "INSERT INTO", "should contain INSERT INTO")
	assert.contains(insert_sql, "builder_insert_test", "should contain table name")
	assert.eq(#args, 3, "should have 3 args")

	-- Test run_with and exec
	local executor = insert:run_with(db)
	assert.not_nil(executor, "should create executor")

	local result, err2 = executor:exec()
	assert.is_nil(err2, "insert exec should not error")
	assert.not_nil(result, "should have result")
	assert.eq(result.rows_affected, 1, "should affect 1 row")

	-- Verify insert
	local check, err3 = db:query("SELECT name, value, active FROM builder_insert_test WHERE name = ?", {"test1"})
	assert.is_nil(err3, "check query should not error")
	assert.eq(#check, 1, "should find inserted row")
	assert.eq(check[1].name, "test1", "name should match")
	assert.eq(check[1].value, 10.5, "value should match")
	assert.eq(check[1].active, 1, "active should match")

	-- Test set_map
	local insert2 = sql.builder.insert("builder_insert_test")
	:set_map({name = "test2", value = 20.5, active = 0})

	local executor2 = insert2:run_with(db)
	local result2, err4 = executor2:exec()
	assert.is_nil(err4, "set_map insert should not error")
	assert.eq(result2.rows_affected, 1, "should affect 1 row")

	-- Verify set_map insert
	local check2, err5 = db:query("SELECT name FROM builder_insert_test WHERE name = ?", {"test2"})
	assert.is_nil(err5, "should find set_map row")
	assert.eq(#check2, 1, "should have 1 row")

	-- Test into method
	local insert3 = sql.builder.insert("")
	:into("builder_insert_test")
	:columns("name", "value", "active")
	:values("test3", 30.0, 1)

	local executor3 = insert3:run_with(db)
	local result3, err6 = executor3:exec()
	assert.is_nil(err6, "into insert should not error")
	assert.eq(result3.rows_affected, 1, "should affect 1 row")

	-- Test with NULL value using sql.NULL
	local insert4 = sql.builder.insert("builder_insert_test")
	:columns("name", "value", "active")
	:values("test_null", sql.NULL, 1)

	local executor4 = insert4:run_with(db)
	local result4, err7 = executor4:exec()
	assert.is_nil(err7, "NULL insert should not error")

	-- Verify NULL
	local check4, err8 = db:query("SELECT value FROM builder_insert_test WHERE name = ?", {"test_null"})
	assert.is_nil(err8, "check NULL should not error")
	assert.is_nil(check4[1].value, "value should be nil/NULL")

	-- Test with typed value sql.as.int
	local insert5 = sql.builder.insert("builder_insert_test")
	:columns("name", "value", "active")
	:values("test_typed", sql.as.float(42.0), sql.as.int(1))

	local executor5 = insert5:run_with(db)
	local result5, err9 = executor5:exec()
	assert.is_nil(err9, "typed insert should not error")

	-- Cleanup
	db:execute("DROP TABLE builder_insert_test")
	db:release()

	return true
end

return { main = main }
