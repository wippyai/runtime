-- Worker that spawns a child, cancels it, and verifies it received CANCEL
local time = require("time")

local function main()
    local events_ch = process.events()
    if not events_ch then
        return false, "failed to get events channel"
    end

    -- Spawn monitored child that waits for cancel
    local child_pid, err = process.spawn_monitored("app.test.process:cancel_verify_worker", "app:processes")
    if err then
        return false, "spawn failed: " .. tostring(err)
    end

    -- Give child time to start and subscribe to events
    time.sleep("50ms")

    -- Send cancel to child
    local cancelled, cancel_err = process.cancel(child_pid, "3s")
    if cancel_err then
        return false, "cancel failed: " .. tostring(cancel_err)
    end

    -- Wait for EXIT event from child
    local timeout = time.after("5s")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= events_ch then
        return false, "timeout waiting for child exit"
    end

    local event = result.value
    if not event then
        return false, "received nil event"
    end

    if event:topic() ~= process.event.EXIT then
        return false, "expected EXIT, got: " .. tostring(event:topic())
    end

    if event:from() ~= child_pid then
        return false, "expected from child " .. child_pid .. ", got: " .. tostring(event:from())
    end

    return true
end

return { main = main }
