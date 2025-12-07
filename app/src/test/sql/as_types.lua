-- Test: sql.as type hints
local assert = require("assert_primitives")

local function main()
    local sql = require("sql")

    local db, err = sql.get("app.test.sql:testdb")
    assert.is_nil(err, "should get database")

    -- Create test table with various types
    local _, err1 = db:execute("CREATE TABLE IF NOT EXISTS as_types_test (id INTEGER PRIMARY KEY, int_val INTEGER, float_val REAL, text_val TEXT, blob_val BLOB)")
    assert.is_nil(err1, "should create table")

    -- Test sql.as.int
    local int_val = sql.as.int(42)
    assert.not_nil(int_val, "should create int type")

    -- Test sql.as.float
    local float_val = sql.as.float(3.14159)
    assert.not_nil(float_val, "should create float type")

    -- Test sql.as.text
    local text_val = sql.as.text("hello world")
    assert.not_nil(text_val, "should create text type")

    -- Test sql.as.binary
    local binary_val = sql.as.binary("binary data")
    assert.not_nil(binary_val, "should create binary type")

    -- Test sql.as.null
    local null_val = sql.as.null()
    assert.not_nil(null_val, "should create null type")

    -- Test insert with typed values
    local insert = sql.builder.insert("as_types_test")
        :columns("int_val", "float_val", "text_val", "blob_val")
        :values(sql.as.int(100), sql.as.float(99.99), sql.as.text("typed text"), sql.as.binary("blob"))

    local executor = insert:run_with(db)
    local result, err2 = executor:exec()
    assert.is_nil(err2, "typed insert should not error")
    assert.eq(result.rows_affected, 1, "should affect 1 row")

    -- Verify inserted values
    local check, err3 = db:query("SELECT int_val, float_val, text_val, blob_val FROM as_types_test")
    assert.is_nil(err3, "check query should not error")
    assert.eq(#check, 1, "should have 1 row")

    -- Integer should be preserved
    local row = check[1]
    assert.eq(type(row.int_val), "number", "int_val should be number")
    assert.eq(row.int_val, 100, "int_val should be 100")

    -- Float should be preserved
    assert.eq(type(row.float_val), "number", "float_val should be number")
    assert.eq(row.float_val, 99.99, "float_val should be 99.99")

    -- Text should be preserved
    assert.eq(type(row.text_val), "string", "text_val should be string")
    assert.eq(row.text_val, "typed text", "text_val should match")

    -- Binary should be preserved
    assert.eq(type(row.blob_val), "string", "blob_val should be string")
    assert.eq(row.blob_val, "blob", "blob_val should match")

    -- Test NULL value
    local insert2 = sql.builder.insert("as_types_test")
        :columns("int_val", "float_val", "text_val", "blob_val")
        :values(sql.as.null(), sql.as.null(), sql.as.null(), sql.as.null())

    local executor2 = insert2:run_with(db)
    local result2, err4 = executor2:exec()
    assert.is_nil(err4, "null insert should not error")

    -- Verify NULLs
    local check2, err5 = db:query("SELECT int_val, float_val, text_val, blob_val FROM as_types_test WHERE int_val IS NULL")
    assert.is_nil(err5, "null check should not error")
    assert.eq(#check2, 1, "should have 1 row with NULLs")
    assert.is_nil(check2[1].int_val, "int_val should be nil")
    assert.is_nil(check2[1].float_val, "float_val should be nil")
    assert.is_nil(check2[1].text_val, "text_val should be nil")
    assert.is_nil(check2[1].blob_val, "blob_val should be nil")

    -- Test sql.NULL constant
    assert.not_nil(sql.NULL, "should have NULL constant")

    local insert3 = sql.builder.insert("as_types_test")
        :columns("int_val", "float_val", "text_val", "blob_val")
        :values(sql.NULL, 1.0, "test", sql.NULL)

    local executor3 = insert3:run_with(db)
    local result3, err6 = executor3:exec()
    assert.is_nil(err6, "sql.NULL insert should not error")

    -- Test integer conversion from float
    local int_from_float = sql.as.int(42.9)
    assert.not_nil(int_from_float, "should convert float to int")

    -- Test in update context
    local update = sql.builder.update("as_types_test")
        :set("int_val", sql.as.int(999))
        :where({id = 1})

    local executor4 = update:run_with(db)
    local result4, err7 = executor4:exec()
    assert.is_nil(err7, "typed update should not error")

    -- Cleanup
    db:execute("DROP TABLE as_types_test")
    db:release()

    return true
end

return { main = main }
