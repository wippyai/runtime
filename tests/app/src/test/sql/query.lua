-- SPDX-License-Identifier: MPL-2.0

-- Test: sql query operations
local assert = require("assert_primitives")

local function main()
	local sql = require("sql")

	local db, err = sql.get("app.test.sql:testdb")
	assert.is_nil(err, "should get database")

	-- Create test table
	local _, err1 = db:execute("CREATE TABLE IF NOT EXISTS query_test (id INTEGER PRIMARY KEY, name TEXT, value REAL)")
	assert.is_nil(err1, "should create table")

	-- Insert test data
	local _, err2 = db:execute("INSERT INTO query_test (name, value) VALUES (?, ?)", {"alice", 1.5})
	assert.is_nil(err2, "should insert row 1")

	local _, err3 = db:execute("INSERT INTO query_test (name, value) VALUES (?, ?)", {"bob", 2.5})
	assert.is_nil(err3, "should insert row 2")

	-- Query all rows (v1 format: array of row tables with column names as keys)
	local rows, err4 = db:query("SELECT id, name, value FROM query_test ORDER BY id")
	assert.is_nil(err4, "query should not error")
	assert.not_nil(rows, "should have rows")

	-- Check rows count
	assert.eq(#rows, 2, "should have 2 rows")

	-- Check first row - access by column name
	local row1 = rows[1]
	assert.eq(type(row1.id), "number", "id should be number")
	assert.eq(row1.name, "alice", "first row name")
	assert.eq(row1.value, 1.5, "first row value")

	-- Check second row
	local row2 = rows[2]
	assert.eq(row2.name, "bob", "second row name")
	assert.eq(row2.value, 2.5, "second row value")

	-- Query with parameters
	local rows2, err5 = db:query("SELECT * FROM query_test WHERE name = ?", {"alice"})
	assert.is_nil(err5, "parameterized query should not error")
	assert.eq(#rows2, 1, "should find 1 row")
	assert.eq(rows2[1].name, "alice", "should find alice")

	-- Empty result
	local rows3, err6 = db:query("SELECT * FROM query_test WHERE name = ?", {"nobody"})
	assert.is_nil(err6, "empty query should not error")
	assert.eq(#rows3, 0, "should have 0 rows")

	-- Cleanup
	db:execute("DROP TABLE query_test")
	db:release()

	return true
end

return { main = main }
