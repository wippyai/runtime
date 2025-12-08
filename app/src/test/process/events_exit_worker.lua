-- Worker process for events_exit test
-- Runs the actual test logic in a process context where process.events() works
local time = require("time")

local function main()
    -- Get our events channel
    local events_ch = process.events()
    if not events_ch then
        return false, "failed to get events channel"
    end

    -- Spawn monitored short-lived worker
    local child_pid, err = process.spawn_monitored("app.test.process:short_worker", "app:processes")
    if err then
        return false, "spawn failed: " .. tostring(err)
    end

    -- Wait for EXIT event
    local timeout = time.after("2s")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= events_ch then
        return false, "timeout waiting for exit event"
    end

    local event = result.value
    if not event then
        return false, "received nil event"
    end

    if event:topic() ~= process.event.EXIT then
        return false, "expected EXIT, got: " .. tostring(event:topic())
    end

    if event:from() ~= child_pid then
        return false, "expected from " .. child_pid .. ", got: " .. tostring(event:from())
    end

    return true
end

return { main = main }
