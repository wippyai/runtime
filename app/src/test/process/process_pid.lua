-- Test: process.pid function
local assert = require("assert2")

local function main()
    -- Test process.pid exists
    assert.not_nil(process.pid, "process.pid exists")
    assert.is_function(process.pid, "process.pid is function")

    -- Get process PID
    local pid = process.pid()
    assert.not_nil(pid, "process.pid returns value")
    assert.is_string(pid, "process.pid is string")

    -- PID should be non-empty string
    assert.ok(#pid > 0, "pid is non-empty")

    -- Calling pid multiple times should return same value
    local pid2 = process.pid()
    assert.eq(pid, pid2, "pid is stable across calls")

    return true
end

return { main = main }
