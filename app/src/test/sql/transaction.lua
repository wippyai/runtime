-- Test: sql transaction operations
local assert = require("assert_primitives")

local function main()
    local sql = require("sql")

    local db, err = sql.get("app.test.sql:testdb")
    assert.is_nil(err, "should get database")

    -- Create test table
    db:execute("CREATE TABLE IF NOT EXISTS tx_test (id INTEGER PRIMARY KEY, name TEXT)")
    db:execute("DELETE FROM tx_test")

    -- Test commit
    local tx1, err1 = db:begin()
    assert.is_nil(err1, "begin should not error")
    assert.not_nil(tx1, "should have transaction")

    -- Check transaction type
    local txtype, err_type = tx1:db_type()
    assert.is_nil(err_type, "db_type should not error")
    assert.eq(txtype, sql.type.SQLITE, "should be sqlite")

    -- Insert in transaction
    local _, err2 = tx1:execute("INSERT INTO tx_test (name) VALUES (?)", {"committed"})
    assert.is_nil(err2, "tx execute should not error")

    -- Commit
    local ok1, err3 = tx1:commit()
    assert.is_nil(err3, "commit should not error")
    assert.eq(ok1, true, "commit should return true")

    -- Verify data persisted
    local result1, _ = db:query("SELECT name FROM tx_test")
    assert.eq(#result1.rows, 1, "should have 1 row after commit")
    assert.eq(result1.rows[1][1], "committed", "should have committed data")

    -- Test rollback
    local tx2, err4 = db:begin()
    assert.is_nil(err4, "second begin should not error")

    tx2:execute("INSERT INTO tx_test (name) VALUES (?)", {"rolled_back"})

    -- Verify data visible in transaction
    local result2, _ = tx2:query("SELECT COUNT(*) FROM tx_test")
    assert.eq(result2.rows[1][1], 2, "should see 2 rows in transaction")

    -- Rollback
    local ok2, err5 = tx2:rollback()
    assert.is_nil(err5, "rollback should not error")
    assert.eq(ok2, true, "rollback should return true")

    -- Verify data not persisted
    local result3, _ = db:query("SELECT COUNT(*) FROM tx_test")
    assert.eq(result3.rows[1][1], 1, "should still have 1 row after rollback")

    -- Test transaction with isolation level
    local tx3, err6 = db:begin({isolation = sql.isolation.SERIALIZABLE})
    assert.is_nil(err6, "begin with isolation should not error")
    tx3:rollback()

    -- Test read-only transaction
    local tx4, err7 = db:begin({read_only = true})
    assert.is_nil(err7, "begin read_only should not error")

    local result4, err8 = tx4:query("SELECT * FROM tx_test")
    assert.is_nil(err8, "read in read_only tx should work")
    tx4:rollback()

    -- Cleanup
    db:execute("DROP TABLE tx_test")
    db:release()

    return true
end

return { main = main }
