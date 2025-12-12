-- Test: SQL query inside nested channel.select completes correctly

local assert = require("assert2")
local sql = require("sql")
local time = require("time")

local function main()
    local db, err = sql.get("app.test.coroutine_sql:testdb")
    assert.is_nil(err, "should get database")

    db:execute("CREATE TABLE IF NOT EXISTS nested_test (id INTEGER PRIMARY KEY, name TEXT)")
    db:execute("DELETE FROM nested_test")
    db:execute("INSERT INTO nested_test (name) VALUES ('nested_item')")
    db:release()

    local ops_channel = channel.new(10)
    local bus_done = channel.new(1)
    local result_channel = channel.new(1)

    coroutine.spawn(function()
        local result = channel.select({
            ops_channel:case_receive()
        })

        if result.ok and result.value then
            local handler_db, handler_err = sql.get("app.test.coroutine_sql:testdb")
            if handler_err then
                result_channel:send({ error = "get_db: " .. handler_err })
                bus_done:send(true)
                return
            end

            local query = sql.builder.select("id", "name")
                :from("nested_test")
                :limit(1)

            local executor = query:run_with(handler_db)
            local rows, query_err = executor:query()
            handler_db:release()

            if query_err then
                result_channel:send({ error = "query: " .. query_err })
            elseif rows and #rows > 0 then
                result_channel:send({ name = rows[1].name })
            else
                result_channel:send({ error = "no rows" })
            end
        end

        bus_done:send(true)
    end)

    ops_channel:send({ type = "test_op" })

    local timeout = time.after("2s")
    local main_result = channel.select({
        result_channel:case_receive(),
        bus_done:case_receive(),
        timeout:case_receive()
    })

    if main_result.channel == timeout then
        error("timeout: nested select SQL query did not complete")
    end

    if main_result.channel == result_channel then
        local data = main_result.value
        if data.error then
            error("handler error: " .. data.error)
        end
        assert.eq(data.name, "nested_item", "should get correct value")
    end

    if main_result.channel == result_channel then
        bus_done:receive()
    end

    return true
end

return { main = main }
