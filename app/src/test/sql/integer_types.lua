-- Test: sql integer type handling (LInteger support)
local assert = require("assert_primitives")

local function main()
    local sql = require("sql")

    local db, err = sql.get("app.test.sql:testdb")
    assert.is_nil(err, "should get database")

    -- Create test table with integer columns
    local _, err1 = db:execute("CREATE TABLE IF NOT EXISTS integer_test (id INTEGER PRIMARY KEY, small_int INTEGER, big_int INTEGER)")
    assert.is_nil(err1, "should create table")

    -- Test inserting integer values
    local insert = sql.builder.insert("integer_test")
        :columns("small_int", "big_int")
        :values(42, 9007199254740991)  -- max safe integer

    local executor = insert:run_with(db)
    local result, err2 = executor:exec()
    assert.is_nil(err2, "integer insert should not error")
    assert.eq(result.rows_affected, 1, "should affect 1 row")

    -- Query and verify integer types
    local check, err3 = db:query("SELECT id, small_int, big_int FROM integer_test")
    assert.is_nil(err3, "query should not error")

    local row = check[1]

    -- ID should be integer
    assert.eq(type(row.id), "number", "id should be number")
    assert.eq(math.type(row.id), "integer", "id should be integer type")

    -- Small int should be integer
    assert.eq(type(row.small_int), "number", "small_int should be number")
    assert.eq(row.small_int, 42, "small_int should be 42")
    assert.eq(math.type(row.small_int), "integer", "small_int should be integer type")

    -- Big int should be integer
    assert.eq(type(row.big_int), "number", "big_int should be number")
    assert.eq(math.type(row.big_int), "integer", "big_int should be integer type")

    -- Test limit with integer
    local query = sql.builder.select("id")
        :from("integer_test")
        :limit(10)

    local q_sql, _ = query:to_sql()
    assert.contains(q_sql, "LIMIT", "should have LIMIT")

    -- Test offset with integer
    local query2 = sql.builder.select("id")
        :from("integer_test")
        :limit(10)
        :offset(5)

    local q2_sql, _ = query2:to_sql()
    assert.contains(q2_sql, "OFFSET", "should have OFFSET")

    -- Test where with integer values
    local query3 = sql.builder.select("id", "small_int")
        :from("integer_test")
        :where({small_int = 42})

    local executor3 = query3:run_with(db)
    local rows3, err4 = executor3:query()
    assert.is_nil(err4, "where integer should not error")
    assert.eq(#rows3, 1, "should find 1 row")
    assert.eq(rows3[1].small_int, 42, "should match integer value")

    -- Test sql.as.int preserves integer type
    local typed_int = sql.as.int(12345)
    assert.not_nil(typed_int, "should create typed int")

    local insert2 = sql.builder.insert("integer_test")
        :columns("small_int", "big_int")
        :values(sql.as.int(100), sql.as.int(200))

    local executor4 = insert2:run_with(db)
    local result4, err5 = executor4:exec()
    assert.is_nil(err5, "typed int insert should not error")

    -- Verify typed integers
    local check2, err6 = db:query("SELECT small_int, big_int FROM integer_test WHERE small_int = 100")
    assert.is_nil(err6, "typed int query should not error")
    assert.eq(#check2, 1, "should find 1 row")
    assert.eq(check2[1].small_int, 100, "small_int should be 100")
    assert.eq(check2[1].big_int, 200, "big_int should be 200")
    assert.eq(math.type(check2[1].small_int), "integer", "should be integer type")

    -- Test update with integer
    local update = sql.builder.update("integer_test")
        :set("small_int", 999)
        :where({id = 1})

    local executor5 = update:run_with(db)
    local result5, err7 = executor5:exec()
    assert.is_nil(err7, "integer update should not error")

    -- Test builder.eq with integer
    local eq_int = sql.builder.eq({small_int = 999})
    local eq_sql, _ = eq_int:to_sql()
    assert.contains(eq_sql, "=", "eq should work with integer")

    -- Test builder.lt with integer
    local lt_int = sql.builder.lt({small_int = 1000})
    local lt_sql, _ = lt_int:to_sql()
    assert.contains(lt_sql, "<", "lt should work with integer")

    -- Test builder.gt with integer
    local gt_int = sql.builder.gt({small_int = 0})
    local gt_sql, _ = gt_int:to_sql()
    assert.contains(gt_sql, ">", "gt should work with integer")

    -- Test result.rows_affected is integer
    assert.eq(math.type(result.rows_affected), "integer", "rows_affected should be integer")

    -- Cleanup
    db:execute("DROP TABLE integer_test")
    db:release()

    return true
end

return { main = main }
