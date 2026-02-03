-- Test: sql builder subquery operations (from_select, insert select)
local assert = require("assert_primitives")

local function main()
	local sql = require("sql")

	local db, err = sql.get("app.test.sql:testdb")
	assert.is_nil(err, "should get database")

	-- Create test tables
	db:execute("CREATE TABLE IF NOT EXISTS subq_source (id INTEGER PRIMARY KEY, name TEXT, value INTEGER)")
	db:execute("CREATE TABLE IF NOT EXISTS subq_target (id INTEGER PRIMARY KEY, name TEXT, value INTEGER)")
	db:execute("DELETE FROM subq_source")
	db:execute("DELETE FROM subq_target")

	-- Insert source data
	db:execute("INSERT INTO subq_source (name, value) VALUES (?, ?)", {"alice", 100})
	db:execute("INSERT INTO subq_source (name, value) VALUES (?, ?)", {"bob", 200})
	db:execute("INSERT INTO subq_source (name, value) VALUES (?, ?)", {"charlie", 300})

	-- Test INSERT ... SELECT (insert:select)
	local subquery = sql.builder.select("name", "value")
	:from("subq_source")
	:where(sql.builder.gt({value = 150}))

	local insert = sql.builder.insert("subq_target")
	:columns("name", "value")
	:select(subquery)

	local insert_sql, _ = insert:to_sql()
	assert.contains(insert_sql, "SELECT", "insert select should contain SELECT")
	assert.contains(insert_sql, "subq_source", "insert select should reference source table")

	local result, err1 = insert:run_with(db):exec()
	assert.is_nil(err1, "insert select should not error")
	assert.eq(result.rows_affected, 2, "should insert 2 rows")

	-- Verify inserted data
	local rows, _ = db:query("SELECT name FROM subq_target ORDER BY name")
	assert.eq(#rows, 2, "should have 2 rows in target")
	assert.eq(rows[1].name, "bob", "first should be bob")
	assert.eq(rows[2].name, "charlie", "second should be charlie")

	-- Test UPDATE ... FROM SELECT (update:from_select)
	local update_subq = sql.builder.select("value")
	:from("subq_source")
	:where({name = "alice"})

	local update = sql.builder.update("subq_target")
	:set("value", 999)
	:from_select(update_subq, "src")
	:where("subq_target.value = src.value")

	local update_sql, _ = update:to_sql()
	assert.contains(update_sql, "FROM", "update from_select should contain FROM")

	-- Test UPDATE ... FROM (simple from)
	local update2 = sql.builder.update("subq_target")
	:set("value", 888)
	:from("subq_source")
	:where("subq_target.name = subq_source.name")

	local update2_sql, _ = update2:to_sql()
	assert.contains(update2_sql, "FROM subq_source", "update from should contain FROM table")

	-- Test complex subquery with multiple clauses
	local complex_subq = sql.builder.select("name", "value")
	:from("subq_source")
	:where(sql.builder.and_({
		sql.builder.gt({value = 50}),
		sql.builder.lt({value = 250})
	}))
	:order_by("value DESC")
	:limit(1)

	local complex_sql, _ = complex_subq:to_sql()
	assert.contains(complex_sql, "AND", "complex subquery should have AND")
	assert.contains(complex_sql, "ORDER BY", "complex subquery should have ORDER BY")
	assert.contains(complex_sql, "LIMIT", "complex subquery should have LIMIT")

	-- Execute the complex subquery
	local complex_rows, err2 = complex_subq:run_with(db):query()
	assert.is_nil(err2, "complex subquery should not error")
	assert.eq(#complex_rows, 1, "should have 1 row")
	assert.eq(complex_rows[1].name, "bob", "should be bob (value 200)")

	-- Test insert with options (prefix)
	local insert_ignore = sql.builder.insert("subq_target")
	:options("OR IGNORE")
	:columns("name", "value")
	:values("bob", 999)

	local ignore_sql, _ = insert_ignore:to_sql()
	assert.contains(ignore_sql, "OR IGNORE", "insert options should contain OR IGNORE")

	-- Cleanup
	db:execute("DROP TABLE subq_source")
	db:execute("DROP TABLE subq_target")
	db:release()

	return true
end

return { main = main }
