local assert = require("assert2")
local registry = require("registry")

local function main()
    -- get nonexistent entry returns NOT_FOUND error
    local entry, err = registry.get("nonexistent:entry")
    assert.is_nil(entry, "expected nil entry")
    assert.not_nil(err, "expected error")
    assert.eq(err:kind(), errors.NOT_FOUND, "error kind is NOT_FOUND")
    assert.eq(err:retryable(), false, "error not retryable")

    -- error has string representation
    local err_str = tostring(err)
    assert.not_nil(err_str, "error has tostring")
    assert.ok(#err_str > 0, "error string not empty")

    -- error can be concatenated (use tostring for nil safety)
    local msg = "Error: " .. tostring(err)
    assert.ok(string.find(msg, "Error:", 1, true), "error can be concatenated")

    return true
end

return { main = main }
