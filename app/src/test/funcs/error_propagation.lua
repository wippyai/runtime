-- Test: Error propagation through funcs.call
local assert = require("assert2")
local funcs = require("funcs")

local function main()
    -- Test calling function that returns structured error
    local result, err = funcs.call("app.test.funcs:error_with_kind", errors.INVALID, "invalid input")
    assert.is_nil(result, "result is nil on error")
    assert.not_nil(err, "error returned from call")
    assert.eq(err:kind(), errors.INVALID, "error kind preserved")
    assert.eq(err:retryable(), false, "error retryable preserved")

    -- Test retryable error propagation
    result, err = funcs.call("app.test.funcs:error_with_kind", errors.UNAVAILABLE, "service down")
    assert.not_nil(err, "error returned")
    assert.eq(err:kind(), errors.UNAVAILABLE, "UNAVAILABLE kind")
    assert.eq(err:retryable(), true, "retryable is true")

    -- Test NOT_FOUND propagation
    result, err = funcs.call("app.test.funcs:error_with_kind", errors.NOT_FOUND, "resource missing")
    assert.not_nil(err, "error returned")
    assert.eq(err:kind(), errors.NOT_FOUND, "NOT_FOUND kind")

    -- Test error propagation via executor
    local exec = funcs.new()
    result, err = exec:call("app.test.funcs:error_with_kind", errors.PERMISSION_DENIED, "access denied")
    assert.not_nil(err, "error from executor call")
    assert.eq(err:kind(), errors.PERMISSION_DENIED, "PERMISSION_DENIED kind via executor")

    -- Test async error propagation preserves kind
    local future = funcs.async("app.test.funcs:error_with_kind", errors.INTERNAL, "internal failure")
    result, err = future:await()
    assert.not_nil(err, "error from async")
    assert.eq(err:kind(), errors.INTERNAL, "INTERNAL kind via async")

    return true
end

return { main = main }
