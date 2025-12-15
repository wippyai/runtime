-- Test: ctx error handling
local assert = require("assert2")
local ctx = require("ctx")

local function main()
    -- Test get nonexistent key
    local val, err = ctx.get("nonexistent_key")
    assert.is_nil(val, "get nonexistent returns nil")
    assert.not_nil(err, "get nonexistent returns error")
    assert.eq(err:kind(), errors.NOT_FOUND, "get nonexistent error kind is NOT_FOUND")
    assert.eq(err:retryable(), false, "get nonexistent error not retryable")

    -- Test get with empty key
    val, err = ctx.get("")
    assert.is_nil(val, "get empty key returns nil")
    assert.not_nil(err, "get empty key returns error")
    assert.eq(err:kind(), errors.INVALID, "get empty key error kind is INVALID")

    return true
end

return { main = main }
