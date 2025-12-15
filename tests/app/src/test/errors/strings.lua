-- Test: error string operations (tostring, concatenation)
local assert = require("assert2")

local function main()
    local e = errors.new("test error")

    -- tostring
    local str = tostring(e)
    assert.ok(str, "tostring returns string")
    assert.contains(str, "test error", "tostring contains message")

    -- string .. error
    local concat1 = "prefix: " .. e
    assert.contains(concat1, "prefix:", "has prefix")
    assert.contains(concat1, "test error", "has error message")

    -- error .. string
    local concat2 = e .. " :suffix"
    assert.contains(concat2, ":suffix", "has suffix")
    assert.contains(concat2, "test error", "has error message")

    -- error .. error
    local e2 = errors.new("another error")
    local concat3 = e .. e2
    assert.contains(concat3, "test error", "has first error")
    assert.contains(concat3, "another error", "has second error")

    return true
end

return { main = main }
