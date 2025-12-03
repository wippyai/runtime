-- Test: internal Lua errors via pcall
local assert = require("assert2")

local function main()
    -- Explicit error() call
    local ok, err = pcall(function()
        error("explicit error")
    end)
    assert.eq(ok, false, "error() should fail pcall")
    assert.contains(err, "explicit error", "error message preserved")

    -- errors.new inside pcall
    local structured_err = errors.new({
        message = "structured inside pcall",
        kind = errors.INVALID
    })
    assert.not_nil(structured_err, "errors.new should return error object")

    ok, err = pcall(function()
        error(structured_err)
    end)
    assert.eq(ok, false, "throwing structured error fails pcall")

    return true
end

return { main = main }
