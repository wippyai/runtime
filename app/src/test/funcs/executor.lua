-- Test: funcs.Executor methods
local assert = require("assert2")
local funcs = require("funcs")

local function main()
    -- Test executor creation
    local exec = funcs.new()
    assert.not_nil(exec, "executor created")

    -- Test with_options
    local exec2 = exec:with_options({ timeout = 1000 })
    assert.not_nil(exec2, "with_options returns executor")
    assert.neq(exec, exec2, "with_options returns new executor")

    -- Test call through executor
    local result, err = exec:call("app.test.funcs:echo", "executor call")
    assert.is_nil(err, "executor call no error")
    assert.not_nil(result, "executor call returns result")
    assert.eq(result.echo, "executor call", "executor call result correct")

    -- Test async through executor
    local future, aerr = exec:async("app.test.funcs:echo", "executor async")
    assert.is_nil(aerr, "executor async no error")
    assert.not_nil(future, "executor async returns future")

    local aresult = future:await()
    assert.eq(aresult:get("echo"), "executor async", "executor async result correct")

    -- Test chaining
    local chained = funcs.new():with_options({ key = "value" })
    assert.not_nil(chained, "chained executor created")

    local cresult = chained:call("app.test.funcs:echo", "chained")
    assert.eq(cresult.echo, "chained", "chained call works")

    return true
end

return { main = main }
