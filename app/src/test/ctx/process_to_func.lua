-- Test: Context inheritance from spawned process to function call
-- Verifies that when a process is spawned with context, and it calls a function
-- via funcs.new():call() (without with_context), the function sees the context
local assert = require("assert2")
local time = require("time")

local function main()
    local events_ch = process.events()

    -- Spawn process with context
    local child_pid, err = process.with_context({
        process_to_func_id = "ptf-321",
        process_called = true
    }):spawn_monitored("app.test.ctx:process_calls_func_worker", "app:processes")

    assert.is_nil(err, "spawn no error")
    assert.not_nil(child_pid, "spawn returns pid")

    -- Wait for worker to exit
    local timeout = time.after("3s")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= events_ch then
        return false, "timeout waiting for worker"
    end

    local event = result.value
    assert.eq(event.kind, process.event.EXIT, "got EXIT event")
    assert.is_nil(event.error, "worker exited without error (context was inherited)")

    return true
end

return { main = main }
