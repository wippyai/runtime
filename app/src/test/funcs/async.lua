-- Test: funcs.async function
local assert = require("assert2")
local funcs = require("funcs")

local function main()
    -- Test funcs.async exists
    assert.not_nil(funcs.async, "funcs.async exists")
    assert.eq(type(funcs.async), "function", "funcs.async is function")

    -- Test async call returns a Future
    local future, err = funcs.async("app.test.funcs:echo", "async test")
    assert.is_nil(err, "funcs.async no error")
    assert.not_nil(future, "funcs.async returns future")

    -- Test Future has await method
    assert.not_nil(future.await, "future has await method")
    assert.eq(type(future.await), "function", "future.await is function")

    -- Test Future has cancel method
    assert.not_nil(future.cancel, "future has cancel method")
    assert.eq(type(future.cancel), "function", "future.cancel is function")

    -- Test Future has channel method
    assert.not_nil(future.channel, "future has channel method")
    assert.eq(type(future.channel), "function", "future.channel is function")

    -- Test awaiting the future returns (value, error)
    local result, err = future:await()
    assert.is_nil(err, "future:await no error")
    assert.not_nil(result, "future:await returns result")
    assert.eq(result.ok, true, "async result ok")
    assert.eq(result.echo, "async test", "async result has input")

    -- Test multiple concurrent async calls
    local f1 = funcs.async("app.test.funcs:echo", "first")
    local f2 = funcs.async("app.test.funcs:echo", "second")
    local f3 = funcs.async("app.test.funcs:echo", "third")

    local r1 = f1:await()
    local r2 = f2:await()
    local r3 = f3:await()

    assert.eq(r1.echo, "first", "first async result")
    assert.eq(r2.echo, "second", "second async result")
    assert.eq(r3.echo, "third", "third async result")

    -- Test executor-based async
    local exec = funcs.new()
    assert.not_nil(exec, "executor created")

    local ef, eerr = exec:async("app.test.funcs:echo", "executor async")
    assert.is_nil(eerr, "executor async no error")
    assert.not_nil(ef, "executor async returns future")

    local er = ef:await()
    assert.eq(er.ok, true, "executor async result ok")
    assert.eq(er.echo, "executor async", "executor async result has input")

    return true
end

return { main = main }
