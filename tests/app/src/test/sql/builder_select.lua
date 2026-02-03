-- Test: sql builder select operations
local assert = require("assert_primitives")

local function main()
	local sql = require("sql")

	local db, err = sql.get("app.test.sql:testdb")
	assert.is_nil(err, "should get database")

	-- Create test table
	local _, err1 = db:execute("CREATE TABLE IF NOT EXISTS builder_select_test (id INTEGER PRIMARY KEY, name TEXT, active INTEGER, score REAL)")
	assert.is_nil(err1, "should create table")

	-- Insert test data
	db:execute("INSERT INTO builder_select_test (name, active, score) VALUES (?, ?, ?)", {"alice", 1, 95.5})
	db:execute("INSERT INTO builder_select_test (name, active, score) VALUES (?, ?, ?)", {"bob", 0, 85.0})
	db:execute("INSERT INTO builder_select_test (name, active, score) VALUES (?, ?, ?)", {"charlie", 1, 90.0})

	-- Test basic select builder
	local query = sql.builder.select("id", "name")
	assert.not_nil(query, "should create select builder")

	-- Test from
	query = query:from("builder_select_test")
	assert.not_nil(query, "should chain from")

	-- Test to_sql
	local query_str, args = query:to_sql()
	assert.not_nil(query_str, "should generate SQL")
	assert.eq(type(query_str), "string", "SQL should be string")
	assert.contains(query_str, "SELECT", "should contain SELECT")
	assert.contains(query_str, "FROM builder_select_test", "should contain FROM")

	-- Test run_with and query execution
	local executor = query:run_with(db)
	assert.not_nil(executor, "should create executor")

	local rows, err2 = executor:query()
	assert.is_nil(err2, "query should not error")
	assert.not_nil(rows, "should have rows")
	assert.eq(#rows, 3, "should have 3 rows")

	-- Test where with table
	local query2 = sql.builder.select("id", "name")
	:from("builder_select_test")
	:where({active = 1})

	local executor2 = query2:run_with(db)
	local rows2, err3 = executor2:query()
	assert.is_nil(err3, "where query should not error")
	assert.eq(#rows2, 2, "should have 2 active rows")

	-- Test where with string condition
	local query3 = sql.builder.select("id", "name", "score")
	:from("builder_select_test")
	:where("score > ?", 88)

	local executor3 = query3:run_with(db)
	local rows3, err4 = executor3:query()
	assert.is_nil(err4, "score filter should not error")
	assert.eq(#rows3, 2, "should have 2 rows with score > 88")

	-- Test order_by
	local query4 = sql.builder.select("name", "score")
	:from("builder_select_test")
	:order_by("score DESC")

	local executor4 = query4:run_with(db)
	local rows4, err5 = executor4:query()
	assert.is_nil(err5, "order by should not error")
	assert.eq(rows4[1].name, "alice", "first row should be alice with highest score")

	-- Test limit
	local query5 = sql.builder.select("name")
	:from("builder_select_test")
	:limit(2)

	local executor5 = query5:run_with(db)
	local rows5, err6 = executor5:query()
	assert.is_nil(err6, "limit should not error")
	assert.eq(#rows5, 2, "should have 2 rows with limit")

	-- Test offset
	local query6 = sql.builder.select("name")
	:from("builder_select_test")
	:order_by("id")
	:limit(1)
	:offset(1)

	local executor6 = query6:run_with(db)
	local rows6, err7 = executor6:query()
	assert.is_nil(err7, "offset should not error")
	assert.eq(#rows6, 1, "should have 1 row")
	assert.eq(rows6[1].name, "bob", "should be second row")

	-- Test distinct
	local query7 = sql.builder.select("active")
	:from("builder_select_test")
	:distinct()

	local executor7 = query7:run_with(db)
	local rows7, err8 = executor7:query()
	assert.is_nil(err8, "distinct should not error")
	assert.eq(#rows7, 2, "should have 2 distinct active values")

	-- Test columns chaining
	local query8 = sql.builder.select("id")
	:from("builder_select_test")
	:columns("name", "score")

	local executor8 = query8:run_with(db)
	local rows8, err9 = executor8:query()
	assert.is_nil(err9, "columns should not error")
	-- Access by column names to verify all 3 columns present
	assert.not_nil(rows8[1].id, "should have id column")
	assert.not_nil(rows8[1].name, "should have name column")
	assert.not_nil(rows8[1].score, "should have score column")

	-- Test group_by
	local query9 = sql.builder.select("active", "COUNT(*) as cnt")
	:from("builder_select_test")
	:group_by("active")

	local executor9 = query9:run_with(db)
	local rows9, err10 = executor9:query()
	assert.is_nil(err10, "group by should not error")
	assert.eq(#rows9, 2, "should have 2 groups")

	-- Test executor to_sql
	local exec_sql, exec_args = executor:to_sql()
	assert.not_nil(exec_sql, "executor to_sql should return SQL")
	assert.not_nil(exec_args, "executor to_sql should return args")

	-- Cleanup
	db:execute("DROP TABLE builder_select_test")
	db:release()

	return true
end

return { main = main }
