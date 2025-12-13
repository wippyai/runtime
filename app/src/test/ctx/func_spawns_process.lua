-- Helper function that spawns a process via process.spawn_monitored() WITHOUT with_context
-- This tests whether context is automatically inherited from function to spawned process
local time = require("time")

local function main()
    local events_ch = process.events()

    -- Spawn process without explicit with_context()
    -- If context inheritance works, the worker should see our context values
    local child_pid, err = process.spawn_monitored(
        "app.test.ctx:func_spawns_process_worker",
        "app:processes"
    )
    if err then
        error("spawn failed: " .. tostring(err))
    end

    -- Wait for worker to exit
    local timeout = time.after("3s")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= events_ch then
        error("timeout waiting for worker")
    end

    local event = result.value
    if event.kind ~= process.event.EXIT then
        error("expected EXIT event")
    end

    if event.error then
        error("worker failed: " .. tostring(event.error))
    end

    return true
end

return { main = main }
