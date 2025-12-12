-- Test: Future methods (is_complete, result, error)
local assert = require("assert2")
local funcs = require("funcs")
local time = require("time")

local function main()
    -- Test is_complete before completion
    local future = funcs.async("app.test.funcs:slow", 100, "test")
    assert.not_nil(future, "future created")

    -- Initially not complete
    local complete = future:is_complete()
    assert.eq(complete, false, "future not complete initially")

    -- result() returns (nil, false) when not complete
    local val, ok = future:result()
    assert.is_nil(val, "result() returns nil value when not complete")
    assert.eq(ok, false, "result() returns false when not complete")

    -- error() returns (nil, false) when not complete
    local err, ok2 = future:error()
    assert.eq(ok2, false, "error() returns false when not complete")
    assert.is_nil(err, "error() returns nil when not complete")

    -- Wait for completion
    local result = future:await()
    assert.not_nil(result, "await returns result")
    assert.eq(result.delayed, true, "result is from slow func")

    -- After completion, is_complete returns true
    complete = future:is_complete()
    assert.eq(complete, true, "future complete after await")

    -- result() returns (value, true) after successful completion
    val, ok = future:result()
    assert.eq(ok, true, "result() returns true after completion")
    assert.not_nil(val, "result() returns value after completion")

    -- error() still returns nil, false (no error)
    err, ok2 = future:error()
    assert.eq(ok2, false, "error() returns false when no error")

    return true
end

return { main = main }
