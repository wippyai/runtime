-- Test: sql builder delete operations
local assert = require("assert_primitives")

local function main()
    local sql = require("sql")

    local db, err = sql.get("app.test.sql:testdb")
    assert.is_nil(err, "should get database")

    -- Create test table
    local _, err1 = db:execute("CREATE TABLE IF NOT EXISTS builder_delete_test (id INTEGER PRIMARY KEY, name TEXT, active INTEGER)")
    assert.is_nil(err1, "should create table")

    -- Insert test data
    db:execute("INSERT INTO builder_delete_test (name, active) VALUES (?, ?)", {"keep1", 1})
    db:execute("INSERT INTO builder_delete_test (name, active) VALUES (?, ?)", {"delete1", 0})
    db:execute("INSERT INTO builder_delete_test (name, active) VALUES (?, ?)", {"keep2", 1})
    db:execute("INSERT INTO builder_delete_test (name, active) VALUES (?, ?)", {"delete2", 0})
    db:execute("INSERT INTO builder_delete_test (name, active) VALUES (?, ?)", {"keep3", 1})

    -- Count before delete
    local before, _ = db:query("SELECT COUNT(*) as cnt FROM builder_delete_test")
    assert.eq(before[1].cnt, 5, "should have 5 rows before delete")

    -- Test basic delete builder
    local delete = sql.builder.delete("builder_delete_test")
    assert.not_nil(delete, "should create delete builder")

    -- Test where
    delete = delete:where({active = 0})

    -- Test to_sql
    local delete_sql, args = delete:to_sql()
    assert.not_nil(delete_sql, "should generate SQL")
    assert.contains(delete_sql, "DELETE FROM", "should contain DELETE FROM")
    assert.contains(delete_sql, "WHERE", "should contain WHERE")

    -- Test run_with and exec
    local executor = delete:run_with(db)
    assert.not_nil(executor, "should create executor")

    local result, err2 = executor:exec()
    assert.is_nil(err2, "delete exec should not error")
    assert.not_nil(result, "should have result")
    assert.eq(result.rows_affected, 2, "should affect 2 rows")

    -- Count after delete
    local after, _ = db:query("SELECT COUNT(*) as cnt FROM builder_delete_test")
    assert.eq(after[1].cnt, 3, "should have 3 rows after delete")

    -- Test where with string condition
    local delete2 = sql.builder.delete("builder_delete_test")
        :where("name LIKE ?", "keep%")
        :limit(1)

    local executor2 = delete2:run_with(db)
    local result2, err3 = executor2:exec()
    assert.is_nil(err3, "string where delete should not error")
    assert.eq(result2.rows_affected, 1, "should affect 1 row")

    -- Test from method
    local delete3 = sql.builder.delete("")
        :from("builder_delete_test")
        :where({name = "keep2"})

    local executor3 = delete3:run_with(db)
    local result3, err4 = executor3:exec()
    assert.is_nil(err4, "from method delete should not error")
    assert.eq(result3.rows_affected, 1, "should affect 1 row")

    -- Verify remaining rows
    local final, _ = db:query("SELECT name FROM builder_delete_test ORDER BY name")
    assert.eq(#final, 1, "should have 1 row remaining")
    assert.eq(final[1].name, "keep3", "should have keep3 remaining")

    -- Cleanup
    db:execute("DROP TABLE builder_delete_test")
    db:release()

    return true
end

return { main = main }
