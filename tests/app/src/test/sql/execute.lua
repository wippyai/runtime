-- SPDX-License-Identifier: MPL-2.0

-- Test: sql execute operations
local assert = require("assert_primitives")

local function main()
	local sql = require("sql")

	local db, err = sql.get("app.test.sql:testdb")
	assert.is_nil(err, "should get database")

	-- Create test table
	local _, err1 = db:execute("CREATE TABLE IF NOT EXISTS execute_test (id INTEGER PRIMARY KEY, name TEXT)")
	assert.is_nil(err1, "should create table")

	-- Insert single row
	local result1, err2 = db:execute("INSERT INTO execute_test (name) VALUES (?)", {"test1"})
	assert.is_nil(err2, "insert should not error")
	assert.not_nil(result1, "should have result")
	assert.eq(type(result1.last_insert_id), "number", "last_insert_id should be number")
	assert.eq(type(result1.rows_affected), "number", "rows_affected should be number")
	assert.eq(result1.rows_affected, 1, "should affect 1 row")

	local first_id = result1.last_insert_id

	-- Insert another row
	local result2, err3 = db:execute("INSERT INTO execute_test (name) VALUES (?)", {"test2"})
	assert.is_nil(err3, "second insert should not error")
	assert.eq(result2.last_insert_id, first_id + 1, "last_insert_id should increment")
	assert.eq(result2.rows_affected, 1, "should affect 1 row")

	-- Update rows
	local result3, err4 = db:execute("UPDATE execute_test SET name = ? WHERE name LIKE ?", {"updated", "test%"})
	assert.is_nil(err4, "update should not error")
	assert.eq(result3.rows_affected, 2, "should update 2 rows")

	-- Delete rows
	local result4, err5 = db:execute("DELETE FROM execute_test WHERE id = ?", {first_id})
	assert.is_nil(err5, "delete should not error")
	assert.eq(result4.rows_affected, 1, "should delete 1 row")

	-- Verify deletion
	local rows, err6 = db:query("SELECT COUNT(*) as cnt FROM execute_test")
	assert.is_nil(err6, "count query should not error")
	local row = rows[1]
	if row then
		assert.eq(row.cnt, 1, "should have 1 row remaining")
	else
		error("expected row from COUNT query")
	end

	-- Cleanup
	db:execute("DROP TABLE execute_test")
	db:release()

	return true
end

return { main = main }
