-- Test: SQL query inside coroutine.spawn completes correctly

local assert = require("assert2")
local sql = require("sql")
local time = require("time")

local function main()
    local db, err = sql.get("app.test.coroutine_sql:testdb")
    assert.is_nil(err, "should get database")

    db:execute("CREATE TABLE IF NOT EXISTS test_items (id INTEGER PRIMARY KEY, name TEXT)")
    db:execute("DELETE FROM test_items")
    db:execute("INSERT INTO test_items (name) VALUES ('test_item')")
    db:release()

    local result_channel = channel.new(1)
    local error_channel = channel.new(1)

    coroutine.spawn(function()
        local coro_db, coro_err = sql.get("app.test.coroutine_sql:testdb")
        if coro_err then
            error_channel:send("get_db error: " .. coro_err)
            return
        end

        local query = sql.builder.select("id", "name")
            :from("test_items")
            :limit(1)

        local executor = query:run_with(coro_db)
        local rows, query_err = executor:query()
        coro_db:release()

        if query_err then
            error_channel:send("query error: " .. query_err)
            return
        end

        if not rows or #rows == 0 then
            error_channel:send("no rows returned")
            return
        end

        result_channel:send(rows[1].name)
    end)

    local timeout = time.after("2s")
    local result = channel.select({
        result_channel:case_receive(),
        error_channel:case_receive(),
        timeout:case_receive()
    })

    if result.channel == timeout then
        error("timeout: coroutine SQL query did not complete")
    end

    if result.channel == error_channel then
        error("coroutine error: " .. result.value)
    end

    assert.eq(result.value, "test_item", "should receive correct value from coroutine")
    return true
end

return { main = main }
