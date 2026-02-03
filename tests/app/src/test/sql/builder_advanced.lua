-- Test: sql builder advanced operations (HAVING, suffix, complex queries)
local assert = require("assert_primitives")

local function main()
	local sql = require("sql")

	local db, err = sql.get("app.test.sql:testdb")
	assert.is_nil(err, "should get database")

	-- Create test table
	db:execute("CREATE TABLE IF NOT EXISTS advanced_test (id INTEGER PRIMARY KEY, category TEXT, amount REAL, status TEXT)")

	-- Insert test data
	db:execute("INSERT INTO advanced_test (category, amount, status) VALUES (?, ?, ?)", {"electronics", 100.0, "active"})
	db:execute("INSERT INTO advanced_test (category, amount, status) VALUES (?, ?, ?)", {"electronics", 200.0, "active"})
	db:execute("INSERT INTO advanced_test (category, amount, status) VALUES (?, ?, ?)", {"electronics", 50.0, "inactive"})
	db:execute("INSERT INTO advanced_test (category, amount, status) VALUES (?, ?, ?)", {"clothing", 30.0, "active"})
	db:execute("INSERT INTO advanced_test (category, amount, status) VALUES (?, ?, ?)", {"clothing", 40.0, "active"})
	db:execute("INSERT INTO advanced_test (category, amount, status) VALUES (?, ?, ?)", {"food", 10.0, "active"})

	-- Test GROUP BY with HAVING
	local query1 = sql.builder.select("category", "SUM(amount) as total")
	:from("advanced_test")
	:where({status = "active"})
	:group_by("category")
	:having("SUM(amount) > ?", 50)
	:order_by("total DESC")

	local rows1, err1 = query1:run_with(db):query()
	assert.is_nil(err1, "having query should not error")
	assert.eq(#rows1, 2, "should have 2 categories with total > 50")
	assert.eq(rows1[1].category, "electronics", "first should be electronics")

	-- Test HAVING with expression
	local query2 = sql.builder.select("category", "COUNT(*) as cnt")
	:from("advanced_test")
	:group_by("category")
	:having(sql.builder.gt({["COUNT(*)"] = 1}))

	local q2_sql, _ = query2:to_sql()
	assert.contains(q2_sql, "HAVING", "should have HAVING")
	assert.contains(q2_sql, ">", "should have > operator")

	-- Test suffix (FOR UPDATE simulation via suffix)
	local query3 = sql.builder.select("id", "category")
	:from("advanced_test")
	:where({status = "active"})
	:suffix("LIMIT 1")

	local q3_sql, _ = query3:to_sql()
	assert.contains(q3_sql, "LIMIT 1", "should have suffix")

	-- Test complex query combining multiple features
	local query4 = sql.builder.select("category", "SUM(amount) as total", "COUNT(*) as cnt")
	:from("advanced_test")
	:where(sql.builder.and_({
		sql.builder.eq({status = "active"}),
		sql.builder.gt({amount = 20})
	}))
	:group_by("category")
	:having("COUNT(*) >= ?", 1)
	:order_by("total DESC")
	:limit(10)

	local rows4, err4 = query4:run_with(db):query()
	assert.is_nil(err4, "complex query should not error")
	assert.eq(#rows4, 2, "should have 2 categories")

	-- Test prefix on INSERT
	local insert = sql.builder.insert("advanced_test")
	:prefix("OR IGNORE")
	:columns("category", "amount", "status")
	:values("books", 25.0, "active")

	local i_sql, _ = insert:to_sql()
	assert.contains(i_sql, "OR IGNORE", "should have prefix")

	-- Test suffix on INSERT
	local insert2 = sql.builder.insert("advanced_test")
	:columns("category", "amount", "status")
	:values("music", 15.0, "active")
	:suffix("RETURNING id")

	local i2_sql, _ = insert2:to_sql()
	assert.contains(i2_sql, "RETURNING", "should have suffix")

	-- Test suffix on UPDATE
	local update = sql.builder.update("advanced_test")
	:set("amount", 999)
	:where({category = "food"})
	:suffix("RETURNING id, amount")

	local u_sql, _ = update:to_sql()
	assert.contains(u_sql, "RETURNING", "update should have suffix")

	-- Test suffix on DELETE
	local delete = sql.builder.delete("advanced_test")
	:where({category = "food"})
	:suffix("RETURNING id")

	local d_sql, _ = delete:to_sql()
	assert.contains(d_sql, "RETURNING", "delete should have suffix")

	-- Cleanup
	db:execute("DROP TABLE advanced_test")
	db:release()

	return true
end

return { main = main }
