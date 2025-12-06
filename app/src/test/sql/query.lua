-- Test: sql query operations
local assert = require("assert_primitives")

local function main()
    local sql = require("sql")

    local db, err = sql.get("app.test.sql:testdb")
    assert.is_nil(err, "should get database")

    -- Create test table
    local _, err1 = db:execute("CREATE TABLE IF NOT EXISTS query_test (id INTEGER PRIMARY KEY, name TEXT, value REAL)")
    assert.is_nil(err1, "should create table")

    -- Insert test data
    local _, err2 = db:execute("INSERT INTO query_test (name, value) VALUES (?, ?)", {"alice", 1.5})
    assert.is_nil(err2, "should insert row 1")

    local _, err3 = db:execute("INSERT INTO query_test (name, value) VALUES (?, ?)", {"bob", 2.5})
    assert.is_nil(err3, "should insert row 2")

    -- Query all rows
    local result, err4 = db:query("SELECT id, name, value FROM query_test ORDER BY id")
    assert.is_nil(err4, "query should not error")
    assert.not_nil(result, "should have result")
    assert.not_nil(result.columns, "should have columns")
    assert.not_nil(result.rows, "should have rows")

    -- Check columns
    assert.eq(#result.columns, 3, "should have 3 columns")
    assert.eq(result.columns[1], "id", "first column is id")
    assert.eq(result.columns[2], "name", "second column is name")
    assert.eq(result.columns[3], "value", "third column is value")

    -- Check rows
    assert.eq(#result.rows, 2, "should have 2 rows")

    -- Check first row - id should be integer
    local row1 = result.rows[1]
    assert.eq(type(row1[1]), "number", "id should be number")
    assert.eq(row1[2], "alice", "first row name")
    assert.eq(row1[3], 1.5, "first row value")

    -- Check second row
    local row2 = result.rows[2]
    assert.eq(row2[2], "bob", "second row name")
    assert.eq(row2[3], 2.5, "second row value")

    -- Query with parameters
    local result2, err5 = db:query("SELECT * FROM query_test WHERE name = ?", {"alice"})
    assert.is_nil(err5, "parameterized query should not error")
    assert.eq(#result2.rows, 1, "should find 1 row")
    assert.eq(result2.rows[1][2], "alice", "should find alice")

    -- Empty result
    local result3, err6 = db:query("SELECT * FROM query_test WHERE name = ?", {"nobody"})
    assert.is_nil(err6, "empty query should not error")
    assert.eq(#result3.rows, 0, "should have 0 rows")

    -- Cleanup
    db:execute("DROP TABLE query_test")
    db:release()

    return true
end

return { main = main }
