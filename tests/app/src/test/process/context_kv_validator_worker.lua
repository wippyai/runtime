-- Worker that validates chained context key/value pairs
local ctx = require("ctx")

local function main()
    -- Validate expected context values from chained with_context calls
    local key1 = ctx.get("key1")
    if key1 ~= "value1" then
        error("key1 mismatch: expected value1, got " .. tostring(key1))
    end

    local key2 = ctx.get("key2")
    if key2 ~= "value2" then
        error("key2 mismatch: expected value2, got " .. tostring(key2))
    end

    return true
end

return { main = main }
