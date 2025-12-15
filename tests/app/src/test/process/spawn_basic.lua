-- Test: Basic process spawn
local assert = require("assert2")
local time = require("time")

local function main()
    -- Test process module exists
    assert.not_nil(process, "process module loaded")
    assert.not_nil(process.spawn, "process.spawn exists")
    assert.not_nil(process.pid, "process.pid exists")

    -- Get our own PID
    local my_pid = process.pid()
    assert.not_nil(my_pid, "got own pid")
    assert.is_string(my_pid, "pid is string")

    -- Test spawning a simple process
    local child_pid, err = process.spawn("app.test.process:echo_worker", "app:processes", "hello")
    assert.is_nil(err, "spawn no error")
    assert.not_nil(child_pid, "spawn returns pid")
    assert.is_string(child_pid, "child pid is string")
    assert.neq(child_pid, my_pid, "child pid differs from parent")

    -- Give process time to complete
    time.sleep("100ms")

    return true
end

return { main = main }
