-- Test: Context inheritance from function call to spawned process
-- Verifies that when function A is called with context, and A spawns a process
-- via process.spawn_monitored() (without with_context), the process sees the context
local assert = require("assert2")
local funcs = require("funcs")

local function main()
    -- Call func_spawns_process with context
    -- It will spawn a process that validates context inheritance
    local exec = funcs.new():with_context({
        func_to_process_id = "ftp-789",
        func_spawned = true
    })

    local result, err = exec:call("app.test.ctx:func_spawns_process")
    assert.is_nil(err, "func_to_process call no error")
    assert.eq(result, true, "func_to_process succeeded")

    return true
end

return { main = main }
