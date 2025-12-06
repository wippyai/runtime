-- Test: sql structured errors
local assert = require("assert_primitives")

local function main()
    local sql = require("sql")

    -- Test invalid resource ID
    local _, err1 = sql.get("")
    assert.not_nil(err1, "empty resource should error")
    assert.eq(err1:kind(), errors.INVALID, "empty resource error kind")
    assert.eq(err1:retryable(), false, "empty resource not retryable")

    -- Test non-existent resource
    local _, err2 = sql.get("nonexistent:db")
    assert.not_nil(err2, "nonexistent resource should error")

    -- Get valid database for further tests
    local db, err = sql.get("app.test.sql:testdb")
    assert.is_nil(err, "should get database")

    -- Test SQL syntax error
    local _, err3 = db:query("INVALID SQL SYNTAX HERE")
    assert.not_nil(err3, "invalid sql should error")

    -- Test query on non-existent table
    local _, err4 = db:query("SELECT * FROM nonexistent_table_xyz")
    assert.not_nil(err4, "query nonexistent table should error")

    -- Test transaction errors
    local tx, _ = db:begin()

    -- Use transaction normally
    tx:execute("CREATE TABLE IF NOT EXISTS err_test (id INTEGER)")

    -- Commit transaction
    tx:commit()

    -- Operations on committed transaction should error
    local _, err5 = tx:query("SELECT 1")
    assert.not_nil(err5, "query on committed tx should error")
    assert.eq(err5:kind(), errors.INVALID, "committed tx error kind")
    assert.eq(err5:retryable(), false, "committed tx not retryable")

    local _, err6 = tx:execute("SELECT 1")
    assert.not_nil(err6, "execute on committed tx should error")

    local _, err7 = tx:commit()
    assert.not_nil(err7, "double commit should error")

    -- Test prepared statement errors
    local stmt, _ = db:prepare("SELECT * FROM err_test WHERE id = ?")

    stmt:close()

    -- Operations on closed statement should error
    local _, err8 = stmt:query({1})
    assert.not_nil(err8, "query on closed stmt should error")
    assert.eq(err8:kind(), errors.INVALID, "closed stmt error kind")

    local _, err9 = stmt:execute({1})
    assert.not_nil(err9, "execute on closed stmt should error")

    local _, err10 = stmt:close()
    assert.not_nil(err10, "double close should error")

    -- Cleanup
    db:execute("DROP TABLE IF EXISTS err_test")
    db:release()

    return true
end

return { main = main }
