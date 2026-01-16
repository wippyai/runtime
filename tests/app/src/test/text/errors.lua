-- Test: text module error handling
local assert = require("assert2")

local function main()
    local text = require("text")

    -- Test regexp compile error - structured error
    local re, err = text.regexp.compile("[invalid")
    assert.is_nil(re, "invalid pattern returns nil")
    assert.not_nil(err, "error returned")
    assert.eq(err:kind(), errors.INVALID, "error kind is INVALID")
    assert.eq(err:retryable(), false, "not retryable")

    -- Test error string representation
    local err_str = tostring(err)
    assert.ok(#err_str > 0, "error has string representation")
    assert.ok(string.find(err_str, "regex", 1, true) or string.find(err_str, "error", 1, true),
        "error message contains context")

    -- Test error concatenation (use tostring for nil safety)
    local msg = "Error: " .. tostring(err)
    assert.ok(string.find(msg, "Error:", 1, true), "error can be concatenated")

    -- Test various invalid patterns
    local patterns = {
        "[",      -- unclosed bracket
        "(?P<>)", -- empty group name
        "*",      -- nothing to repeat
    }

    for _, pattern in ipairs(patterns) do
        local r, e = text.regexp.compile(pattern)
        assert.is_nil(r, "invalid pattern '" .. pattern .. "' returns nil")
        assert.not_nil(e, "invalid pattern '" .. pattern .. "' returns error")
        assert.eq(e:kind(), errors.INVALID, "error kind is INVALID for '" .. pattern .. "'")
    end

    return true
end

return { main = main }
