-- SPDX-License-Identifier: MPL-2.0

-- Test: sql builder JOIN operations
local assert = require("assert_primitives")

local function main()
	local sql = require("sql")

	local db, err = sql.get("app.test.sql:testdb")
	assert.is_nil(err, "should get database")

	-- Create test tables
	db:execute("CREATE TABLE IF NOT EXISTS users_join (id INTEGER PRIMARY KEY, name TEXT)")
	db:execute("CREATE TABLE IF NOT EXISTS orders_join (id INTEGER PRIMARY KEY, user_id INTEGER, product TEXT)")
	db:execute("CREATE TABLE IF NOT EXISTS profiles_join (id INTEGER PRIMARY KEY, user_id INTEGER, bio TEXT)")

	-- Insert test data
	db:execute("INSERT INTO users_join (id, name) VALUES (?, ?)", {1, "alice"})
	db:execute("INSERT INTO users_join (id, name) VALUES (?, ?)", {2, "bob"})
	db:execute("INSERT INTO users_join (id, name) VALUES (?, ?)", {3, "charlie"})

	db:execute("INSERT INTO orders_join (user_id, product) VALUES (?, ?)", {1, "laptop"})
	db:execute("INSERT INTO orders_join (user_id, product) VALUES (?, ?)", {1, "phone"})
	db:execute("INSERT INTO orders_join (user_id, product) VALUES (?, ?)", {2, "tablet"})

	db:execute("INSERT INTO profiles_join (user_id, bio) VALUES (?, ?)", {1, "Developer"})
	db:execute("INSERT INTO profiles_join (user_id, bio) VALUES (?, ?)", {3, "Designer"})

	-- Test basic JOIN
	local query1 = sql.builder.select("users_join.name", "orders_join.product")
	:from("users_join")
	:join("orders_join", "users_join.id = orders_join.user_id")
	:order_by("users_join.name", "orders_join.product")

	local rows1, err1 = query1:run_with(db):query()
	assert.is_nil(err1, "join query should not error")
	assert.eq(#rows1, 3, "should have 3 joined rows")
	assert.eq(rows1[1].name, "alice", "first should be alice")

	-- Test LEFT JOIN (users without orders included)
	local query2 = sql.builder.select("users_join.name", "orders_join.product")
	:from("users_join")
	:left_join("orders_join", "users_join.id = orders_join.user_id")
	:order_by("users_join.name")

	local rows2, err2 = query2:run_with(db):query()
	assert.is_nil(err2, "left join should not error")
	assert.eq(#rows2, 4, "should have 4 rows (charlie has no order)")

	-- Test RIGHT JOIN
	local query3 = sql.builder.select("users_join.name", "profiles_join.bio")
	:from("users_join")
	:right_join("profiles_join", "users_join.id = profiles_join.user_id")
	:order_by("users_join.name")

	local q3_sql, _ = query3:to_sql()
	assert.contains(q3_sql, "RIGHT JOIN", "should have RIGHT JOIN")

	-- Test INNER JOIN
	local query4 = sql.builder.select("users_join.name", "orders_join.product")
	:from("users_join")
	:inner_join("orders_join", "users_join.id = orders_join.user_id")

	local q4_sql, _ = query4:to_sql()
	assert.contains(q4_sql, "INNER JOIN", "should have INNER JOIN")

	-- Test multiple JOINs
	local query5 = sql.builder.select("users_join.name", "orders_join.product", "profiles_join.bio")
	:from("users_join")
	:left_join("orders_join", "users_join.id = orders_join.user_id")
	:left_join("profiles_join", "users_join.id = profiles_join.user_id")
	:where({["users_join.id"] = 1})

	local rows5, err5 = query5:run_with(db):query()
	assert.is_nil(err5, "multiple joins should not error")
	assert.eq(#rows5, 2, "alice has 2 orders with profile")

	-- Cleanup
	db:execute("DROP TABLE users_join")
	db:execute("DROP TABLE orders_join")
	db:execute("DROP TABLE profiles_join")
	db:release()

	return true
end

return { main = main }
