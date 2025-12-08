-- Tests that LINK_DOWN is sent when parent exits with error
local time = require("time")

local function main()
    local events_ch = process.events()
    if not events_ch then
        return false, "failed to get events channel"
    end

    -- Spawn linked child that will wait for events
    local child_pid, err = process.spawn_linked_monitored("app.test.process:linked_child_worker", "app:processes")
    if err then
        return false, "spawn linked child failed: " .. tostring(err)
    end

    -- Give child time to start
    time.sleep("50ms")

    -- Spawn linked error worker - when it errors, LINK_DOWN should propagate to child
    local error_pid, err2 = process.spawn_linked("app.test.process:error_exit_worker", "app:processes")
    if err2 then
        return false, "spawn error worker failed: " .. tostring(err2)
    end

    -- Wait for EXIT event from error worker (we monitor it indirectly through linked child)
    -- The error_exit_worker will error, triggering LINK_DOWN to us (this worker)
    -- We are also linked to child, so when we exit, child gets LINK_DOWN too
    local timeout = time.after("3s")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel == events_ch then
        local event = result.value
        if event and event:topic() == process.event.LINK_DOWN then
            return true
        end
        return false, "expected LINK_DOWN, got: " .. tostring(event and event:topic() or "nil")
    end

    return false, "timeout waiting for LINK_DOWN"
end

return { main = main }
