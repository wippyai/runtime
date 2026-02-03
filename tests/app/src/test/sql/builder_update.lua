-- Test: sql builder update operations
local assert = require("assert_primitives")

local function main()
	local sql = require("sql")

	local db, err = sql.get("app.test.sql:testdb")
	assert.is_nil(err, "should get database")

	-- Create test table
	local _, err1 = db:execute("CREATE TABLE IF NOT EXISTS builder_update_test (id INTEGER PRIMARY KEY, name TEXT, value REAL, active INTEGER)")
	assert.is_nil(err1, "should create table")

	-- Insert test data
	db:execute("INSERT INTO builder_update_test (name, value, active) VALUES (?, ?, ?)", {"original", 100.0, 1})
	db:execute("INSERT INTO builder_update_test (name, value, active) VALUES (?, ?, ?)", {"keep", 200.0, 1})
	db:execute("INSERT INTO builder_update_test (name, value, active) VALUES (?, ?, ?)", {"another", 300.0, 0})

	-- Test basic update builder
	local update = sql.builder.update("builder_update_test")
	assert.not_nil(update, "should create update builder")

	-- Test set and where
	update = update:set("name", "updated")
	:set("value", 150.0)
	:where({id = 1})

	-- Test to_sql
	local update_sql, args = update:to_sql()
	assert.not_nil(update_sql, "should generate SQL")
	assert.contains(update_sql, "UPDATE", "should contain UPDATE")
	assert.contains(update_sql, "SET", "should contain SET")

	-- Test run_with and exec
	local executor = update:run_with(db)
	assert.not_nil(executor, "should create executor")

	local result, err2 = executor:exec()
	assert.is_nil(err2, "update exec should not error")
	assert.not_nil(result, "should have result")
	assert.eq(result.rows_affected, 1, "should affect 1 row")

	-- Verify update
	local check, err3 = db:query("SELECT name, value FROM builder_update_test WHERE id = ?", {1})
	assert.is_nil(err3, "check query should not error")
	assert.eq(check[1].name, "updated", "name should be updated")
	assert.eq(check[1].value, 150.0, "value should be updated")

	-- Test set_map
	local update2 = sql.builder.update("builder_update_test")
	:set_map({name = "batch_updated", value = 999.0})
	:where({active = 1})

	local executor2 = update2:run_with(db)
	local result2, err4 = executor2:exec()
	assert.is_nil(err4, "set_map update should not error")
	assert.eq(result2.rows_affected, 2, "should affect 2 active rows")

	-- Test where with string condition
	local update3 = sql.builder.update("builder_update_test")
	:set("active", 0)
	:where("value > ?", 500)

	local executor3 = update3:run_with(db)
	local result3, err5 = executor3:exec()
	assert.is_nil(err5, "string where update should not error")
	assert.eq(result3.rows_affected, 1, "should affect 1 row with value > 500")

	-- Test with NULL using sql.NULL
	local update4 = sql.builder.update("builder_update_test")
	:set("value", sql.NULL)
	:where({id = 3})

	local executor4 = update4:run_with(db)
	local result4, err6 = executor4:exec()
	assert.is_nil(err6, "NULL update should not error")

	-- Verify NULL
	local check4, err7 = db:query("SELECT value FROM builder_update_test WHERE id = ?", {3})
	assert.is_nil(err7, "check NULL should not error")
	assert.is_nil(check4[1].value, "value should be NULL")

	-- Test limit (may not be supported by all DBs)
	local update5 = sql.builder.update("builder_update_test")
	:set("active", 1)
	:where({active = 0})
	:limit(1)

	local _, args5 = update5:to_sql()
	assert.not_nil(args5, "limit update should generate SQL")

	-- Test table method
	local update6 = sql.builder.update("")
	:table("builder_update_test")
	:set("name", "via_table_method")
	:where({id = 1})

	local executor6 = update6:run_with(db)
	local result6, err8 = executor6:exec()
	assert.is_nil(err8, "table method update should not error")

	-- Cleanup
	db:execute("DROP TABLE builder_update_test")
	db:release()

	return true
end

return { main = main }
