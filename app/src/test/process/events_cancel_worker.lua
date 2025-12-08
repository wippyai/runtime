-- Worker process for events_cancel test
-- Runs cancel logic in a process context
local time = require("time")

local function main()
    -- Get our events channel
    local events_ch = process.events()
    if not events_ch then
        return false, "failed to get events channel"
    end

    -- Spawn monitored long-running worker
    local child_pid, err = process.spawn_monitored("app.test.process:long_worker", "app:processes")
    if err then
        return false, "spawn failed: " .. tostring(err)
    end

    -- Give process time to start
    time.sleep("50ms")

    -- Cancel the child process
    local cancelled, cancel_err = process.cancel(child_pid, "1s")
    if cancel_err then
        return false, "cancel failed: " .. tostring(cancel_err)
    end

    -- Wait for EXIT event (child should exit after handling cancel)
    local timeout = time.after("3s")
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

    return true
end

return { main = main }
