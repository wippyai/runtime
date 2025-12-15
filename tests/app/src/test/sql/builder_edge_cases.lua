-- Test: sql builder edge cases and comprehensive coverage
local assert = require("assert_primitives")

local function main()
    local sql = require("sql")

    local db, err = sql.get("app.test.sql:testdb")
    assert.is_nil(err, "should get database")

    -- Create test table
    db:execute("CREATE TABLE IF NOT EXISTS edge_test (id INTEGER PRIMARY KEY, name TEXT, value REAL, active INTEGER, category TEXT)")
    db:execute("DELETE FROM edge_test")

    -- Insert test data
    db:execute("INSERT INTO edge_test (name, value, active, category) VALUES (?, ?, ?, ?)", {"alice", 100.5, 1, "A"})
    db:execute("INSERT INTO edge_test (name, value, active, category) VALUES (?, ?, ?, ?)", {"bob", 200.0, 0, "B"})
    db:execute("INSERT INTO edge_test (name, value, active, category) VALUES (?, ?, ?, ?)", {"charlie", 150.75, 1, "A"})
    db:execute("INSERT INTO edge_test (name, value, active, category) VALUES (?, ?, ?, ?)", {"diana", 300.0, 1, "C"})

    -- Test SELECT with all clauses combined
    local full_select = sql.builder.select("name", "value", "category")
        :distinct()
        :from("edge_test")
        :where({active = 1})
        :group_by("category")
        :having("COUNT(*) >= ?", 1)
        :order_by("category ASC")
        :limit(10)
        :offset(0)

    local full_sql, full_args = full_select:to_sql()
    assert.contains(full_sql, "DISTINCT", "should have DISTINCT")
    assert.contains(full_sql, "WHERE", "should have WHERE")
    assert.contains(full_sql, "GROUP BY", "should have GROUP BY")
    assert.contains(full_sql, "HAVING", "should have HAVING")
    assert.contains(full_sql, "ORDER BY", "should have ORDER BY")
    assert.contains(full_sql, "LIMIT", "should have LIMIT")
    assert.contains(full_sql, "OFFSET", "should have OFFSET")
    assert.eq(#full_args, 2, "should have 2 args (active and having)")

    -- Test multiple WHERE conditions (AND chaining)
    local multi_where = sql.builder.select("*")
        :from("edge_test")
        :where({active = 1})
        :where({category = "A"})
        :where("value > ?", 100)

    local rows1, err1 = multi_where:run_with(db):query()
    assert.is_nil(err1, "multi where should not error")
    assert.eq(#rows1, 1, "should have 1 row matching all conditions")
    assert.eq(rows1[1].name, "charlie", "should be charlie")

    -- Test ORDER BY with multiple columns
    local multi_order = sql.builder.select("name", "category", "value")
        :from("edge_test")
        :order_by("category ASC", "value DESC")

    local rows2, _ = multi_order:run_with(db):query()
    assert.eq(rows2[1].category, "A", "first category should be A")
    assert.eq(rows2[1].name, "charlie", "first in A should be charlie (higher value)")

    -- Test columns method to add more columns
    local cols_query = sql.builder.select("id")
        :columns("name")
        :columns("value", "category")
        :from("edge_test")
        :limit(1)

    local cols_sql, _ = cols_query:to_sql()
    assert.contains(cols_sql, "id", "should have id")
    assert.contains(cols_sql, "name", "should have name")
    assert.contains(cols_sql, "value", "should have value")
    assert.contains(cols_sql, "category", "should have category")

    -- Test UPDATE with multiple SET
    local multi_set = sql.builder.update("edge_test")
        :set("value", 999)
        :set("active", 0)
        :set("category", "X")
        :where({name = "diana"})

    local update_result, err2 = multi_set:run_with(db):exec()
    assert.is_nil(err2, "multi set should not error")
    assert.eq(update_result.rows_affected, 1, "should update 1 row")

    -- Verify update
    local verify, _ = db:query("SELECT value, active, category FROM edge_test WHERE name = ?", {"diana"})
    assert.eq(verify[1].value, 999, "value should be 999")
    assert.eq(verify[1].active, 0, "active should be 0")
    assert.eq(verify[1].category, "X", "category should be X")

    -- Test DELETE with ORDER BY and LIMIT
    local ordered_delete = sql.builder.delete("edge_test")
        :where({active = 0})
        :order_by("value ASC")
        :limit(1)

    local del_sql, _ = ordered_delete:to_sql()
    assert.contains(del_sql, "ORDER BY", "delete should have ORDER BY")
    assert.contains(del_sql, "LIMIT", "delete should have LIMIT")

    -- Test expression combinations
    local expr_combo = sql.builder.select("name")
        :from("edge_test")
        :where(sql.builder.or_({
            sql.builder.and_({
                sql.builder.eq({category = "A"}),
                sql.builder.gt({value = 100})
            }),
            sql.builder.eq({name = "diana"})
        }))

    local expr_sql, _ = expr_combo:to_sql()
    assert.contains(expr_sql, "OR", "should have OR")
    assert.contains(expr_sql, "AND", "should have AND")

    -- Test raw expression with multiple parameters
    local raw_expr = sql.builder.expr("value BETWEEN ? AND ? AND category IN (?, ?)", 100, 300, "A", "C")
    local raw_sql, raw_args = raw_expr:to_sql()
    assert.eq(raw_sql, "value BETWEEN ? AND ? AND category IN (?, ?)", "raw expr should preserve SQL")
    assert.eq(#raw_args, 4, "raw expr should have 4 args")

    -- Test builder __tostring metamethod
    local tostring_query = sql.builder.select("id"):from("edge_test")
    local str = tostring(tostring_query)
    assert.contains(str, "SELECT", "__tostring should contain SELECT")
    assert.contains(str, "edge_test", "__tostring should contain table name")

    -- Test INSERT with prefix and suffix
    local insert_full = sql.builder.insert("edge_test")
        :prefix("OR REPLACE")
        :columns("name", "value", "active", "category")
        :values("eve", 400, 1, "D")
        :suffix("RETURNING id")

    local insert_sql, _ = insert_full:to_sql()
    assert.contains(insert_sql, "OR REPLACE", "should have prefix")
    assert.contains(insert_sql, "RETURNING", "should have suffix")

    -- Test UPDATE with suffix
    local update_suffix = sql.builder.update("edge_test")
        :set("value", 111)
        :where({name = "alice"})
        :suffix("RETURNING value")

    local update_suffix_sql, _ = update_suffix:to_sql()
    assert.contains(update_suffix_sql, "RETURNING", "update should have suffix")

    -- Test empty results handling
    local empty_query = sql.builder.select("*")
        :from("edge_test")
        :where({name = "nonexistent"})

    local empty_rows, err3 = empty_query:run_with(db):query()
    assert.is_nil(err3, "empty query should not error")
    assert.eq(#empty_rows, 0, "should have 0 rows")

    -- Test NULL comparison with expressions
    db:execute("INSERT INTO edge_test (name, value, active, category) VALUES (?, ?, ?, ?)", {"null_test", sql.NULL, 1, sql.NULL})

    local null_query = sql.builder.select("name")
        :from("edge_test")
        :where("value IS NULL")

    local null_rows, _ = null_query:run_with(db):query()
    assert.eq(#null_rows, 1, "should find 1 row with NULL value")
    assert.eq(null_rows[1].name, "null_test", "should be null_test")

    -- Test executor to_sql returns same as builder to_sql
    local test_builder = sql.builder.select("id"):from("edge_test"):where({active = 1})
    local builder_sql, builder_args = test_builder:to_sql()

    local executor = test_builder:run_with(db)
    local exec_sql, exec_args = executor:to_sql()

    assert.eq(builder_sql, exec_sql, "executor to_sql should match builder to_sql")
    assert.eq(#builder_args, #exec_args, "args count should match")

    -- Cleanup
    db:execute("DROP TABLE edge_test")
    db:release()

    return true
end

return { main = main }
