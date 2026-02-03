-- Test: sql builder expression operations
local assert = require("assert_primitives")

local function main()
	local sql = require("sql")

	local db, err = sql.get("app.test.sql:testdb")
	assert.is_nil(err, "should get database")

	-- Create test table
	local _, err1 = db:execute("CREATE TABLE IF NOT EXISTS builder_expr_test (id INTEGER PRIMARY KEY, name TEXT, score INTEGER, active INTEGER)")
	assert.is_nil(err1, "should create table")

	-- Insert test data
	db:execute("INSERT INTO builder_expr_test (name, score, active) VALUES (?, ?, ?)", {"alice", 95, 1})
	db:execute("INSERT INTO builder_expr_test (name, score, active) VALUES (?, ?, ?)", {"bob", 80, 1})
	db:execute("INSERT INTO builder_expr_test (name, score, active) VALUES (?, ?, ?)", {"charlie", 70, 0})
	db:execute("INSERT INTO builder_expr_test (name, score, active) VALUES (?, ?, ?)", {"diana", 85, 1})
	db:execute("INSERT INTO builder_expr_test (name, score, active) VALUES (?, ?, ?)", {"eve", 90, 0})

	-- Test eq expression
	local eq = sql.builder.eq({active = 1})
	assert.not_nil(eq, "should create eq expression")
	local eq_sql, eq_args = eq:to_sql()
	assert.contains(eq_sql, "=", "eq should contain =")
	assert.eq(#eq_args, 1, "eq should have 1 arg")

	-- Test not_eq expression
	local not_eq = sql.builder.not_eq({active = 0})
	assert.not_nil(not_eq, "should create not_eq expression")
	local not_eq_sql, _ = not_eq:to_sql()
	assert.contains(not_eq_sql, "<>", "not_eq should contain <>")

	-- Test lt expression
	local lt = sql.builder.lt({score = 85})
	assert.not_nil(lt, "should create lt expression")
	local lt_sql, _ = lt:to_sql()
	assert.contains(lt_sql, "<", "lt should contain <")

	-- Test gt expression
	local gt = sql.builder.gt({score = 85})
	assert.not_nil(gt, "should create gt expression")
	local gt_sql, _ = gt:to_sql()
	assert.contains(gt_sql, ">", "gt should contain >")

	-- Test lte expression
	local lte = sql.builder.lte({score = 85})
	assert.not_nil(lte, "should create lte expression")
	local lte_sql, _ = lte:to_sql()
	assert.contains(lte_sql, "<=", "lte should contain <=")

	-- Test gte expression
	local gte = sql.builder.gte({score = 85})
	assert.not_nil(gte, "should create gte expression")
	local gte_sql, _ = gte:to_sql()
	assert.contains(gte_sql, ">=", "gte should contain >=")

	-- Test like expression
	local like = sql.builder.like({name = "a%"})
	assert.not_nil(like, "should create like expression")
	local like_sql, _ = like:to_sql()
	assert.contains(like_sql, "LIKE", "like should contain LIKE")

	-- Test not_like expression
	local not_like = sql.builder.not_like({name = "z%"})
	assert.not_nil(not_like, "should create not_like expression")
	local not_like_sql, _ = not_like:to_sql()
	assert.contains(not_like_sql, "NOT LIKE", "not_like should contain NOT LIKE")

	-- Test expr for raw SQL
	local expr = sql.builder.expr("score BETWEEN ? AND ?", 80, 90)
	assert.not_nil(expr, "should create raw expression")
	local expr_sql, expr_args = expr:to_sql()
	assert.eq(expr_sql, "score BETWEEN ? AND ?", "expr should preserve SQL")
	assert.eq(#expr_args, 2, "expr should have 2 args")

	-- Test and_ expression
	local and_expr = sql.builder.and_({
		sql.builder.eq({active = 1}),
		sql.builder.gt({score = 80})
	})
	assert.not_nil(and_expr, "should create and expression")
	local and_sql, _ = and_expr:to_sql()
	assert.contains(and_sql, "AND", "and_ should contain AND")

	-- Test or_ expression
	local or_expr = sql.builder.or_({
		sql.builder.eq({active = 0}),
		sql.builder.gt({score = 90})
	})
	assert.not_nil(or_expr, "should create or expression")
	local or_sql, _ = or_expr:to_sql()
	assert.contains(or_sql, "OR", "or_ should contain OR")

	-- Test using expressions in select
	local query = sql.builder.select("name", "score")
	:from("builder_expr_test")
	:where(sql.builder.and_({
		sql.builder.eq({active = 1}),
		sql.builder.gte({score = 85})
	}))
	:order_by("score DESC")

	local executor = query:run_with(db)
	local rows, err2 = executor:query()
	assert.is_nil(err2, "expression query should not error")
	assert.eq(#rows, 2, "should find 2 matching rows")
	assert.eq(rows[1].name, "alice", "first should be alice")
	assert.eq(rows[2].name, "diana", "second should be diana")

	-- Test or_ with tables (shorthand for eq)
	local or_table = sql.builder.or_({
		{name = "alice"},
		{name = "bob"}
	})
	local or_table_sql, _ = or_table:to_sql()
	assert.contains(or_table_sql, "OR", "or_ with tables should work")

	-- Test placeholder formats
	assert.not_nil(sql.builder.question, "should have question placeholder")
	assert.not_nil(sql.builder.dollar, "should have dollar placeholder")
	assert.not_nil(sql.builder.at, "should have at placeholder")
	assert.not_nil(sql.builder.colon, "should have colon placeholder")
	assert.not_nil(sql.builder.default_placeholder, "should have default placeholder")

	-- Test question placeholder (default for MySQL/SQLite)
	local q_query = sql.builder.select("id", "name")
	:from("builder_expr_test")
	:where({id = 1})
	:where({name = "test"})
	:placeholder_format(sql.builder.question)

	local q_sql, _ = q_query:to_sql()
	assert.contains(q_sql, "?", "question placeholder should use ?")

	-- Test dollar placeholder (PostgreSQL style)
	local pg_query = sql.builder.select("id")
	:from("builder_expr_test")
	:where({id = 1})
	:placeholder_format(sql.builder.dollar)

	local pg_sql, _ = pg_query:to_sql()
	assert.contains(pg_sql, "$1", "dollar placeholder should use $1")

	-- Test at placeholder (@p1, @p2 style - SQL Server)
	local at_query = sql.builder.select("id", "name")
	:from("builder_expr_test")
	:where({id = 1})
	:where({name = "test"})
	:placeholder_format(sql.builder.at)

	local at_sql, _ = at_query:to_sql()
	assert.contains(at_sql, "@p1", "at placeholder should use @p1")
	assert.contains(at_sql, "@p2", "at placeholder should use @p2")

	-- Test colon placeholder (:1, :2 style - Oracle)
	local colon_query = sql.builder.select("id", "name")
	:from("builder_expr_test")
	:where({id = 1})
	:where({name = "test"})
	:placeholder_format(sql.builder.colon)

	local colon_sql, _ = colon_query:to_sql()
	assert.contains(colon_sql, ":1", "colon placeholder should use :1")
	assert.contains(colon_sql, ":2", "colon placeholder should use :2")

	-- Test placeholder on INSERT
	local insert_pg = sql.builder.insert("builder_expr_test")
	:columns("name", "score", "active")
	:values("test", 100, 1)
	:placeholder_format(sql.builder.dollar)

	local insert_pg_sql, _ = insert_pg:to_sql()
	assert.contains(insert_pg_sql, "$1", "insert dollar should use $1")
	assert.contains(insert_pg_sql, "$2", "insert dollar should use $2")
	assert.contains(insert_pg_sql, "$3", "insert dollar should use $3")

	-- Test placeholder on UPDATE
	local update_at = sql.builder.update("builder_expr_test")
	:set("name", "updated")
	:where({id = 1})
	:placeholder_format(sql.builder.at)

	local update_at_sql, _ = update_at:to_sql()
	assert.contains(update_at_sql, "@p1", "update at should use @p1")
	assert.contains(update_at_sql, "@p2", "update at should use @p2")

	-- Test placeholder on DELETE
	local delete_colon = sql.builder.delete("builder_expr_test")
	:where({id = 1})
	:placeholder_format(sql.builder.colon)

	local delete_colon_sql, _ = delete_colon:to_sql()
	assert.contains(delete_colon_sql, ":1", "delete colon should use :1")

	-- Cleanup
	db:execute("DROP TABLE builder_expr_test")
	db:release()

	return true
end

return { main = main }
