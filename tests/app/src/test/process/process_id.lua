-- Test: process.id function
local assert = require("assert2")

local function main()
    -- Test process.id exists
    assert.not_nil(process.id, "process.id exists")
    assert.is_function(process.id, "process.id is function")

    -- Get process ID (registry ID, not PID)
    local id, err = process.id()
    assert.is_nil(err, "process.id no error")
    assert.not_nil(id, "process.id returns value")
    assert.is_string(id, "process.id is string")

    -- Process ID should be a registry ID format (namespace:name)
    assert.ok(string.find(id, ":"), "id contains namespace separator")

    return true
end

return { main = main }
