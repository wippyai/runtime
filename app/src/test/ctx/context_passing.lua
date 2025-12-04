-- Test: Context passing through funcs.call
local assert = require("assert2")
local ctx = require("ctx")
local funcs = require("funcs")

local function main()
    -- Test context passing through funcs.new():with_context():call()
    local exec = funcs.new():with_context({
        request_id = "req-123",
        user_id = 42,
        is_admin = true
    })

    local result, err = exec:call("app.test.ctx:ctx_reader", { "request_id", "user_id", "is_admin" })
    assert.is_nil(err, "call with context no error")
    assert.not_nil(result, "call with context returns result")

    assert.eq(result.request_id, "req-123", "context request_id passed")
    assert.eq(result.user_id, 42, "context user_id passed")
    assert.eq(result.is_admin, true, "context is_admin passed")

    -- Test chaining with_context calls
    local exec2 = funcs.new()
        :with_context({ key1 = "value1" })
        :with_context({ key2 = "value2" })

    local result2, err2 = exec2:call("app.test.ctx:ctx_reader", { "key1", "key2" })
    assert.is_nil(err2, "chained context call no error")
    assert.eq(result2.key1, "value1", "chained context key1 passed")
    assert.eq(result2.key2, "value2", "chained context key2 passed")

    -- Test context with complex values (tables)
    local exec3 = funcs.new():with_context({
        config = { max_retries = 3, timeout = 1000 }
    })

    local result3, err3 = exec3:call("app.test.ctx:ctx_reader", "config")
    assert.is_nil(err3, "context with table no error")
    assert.eq(type(result3.config), "table", "context table is table")
    assert.eq(result3.config.max_retries, 3, "context table.max_retries correct")
    assert.eq(result3.config.timeout, 1000, "context table.timeout correct")

    return true
end

return { main = main }
