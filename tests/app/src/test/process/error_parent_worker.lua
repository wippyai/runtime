-- Parent that spawns child, waits for it to link, then errors
local time = require("time")

local function main()
    local events_ch = process.events()
    if not events_ch then
        return false, "failed to get events channel"
    end

    local inbox_ch = process.inbox()
    if not inbox_ch then
        return false, "failed to get inbox channel"
    end

    -- Spawn child
    local child_pid, err = process.spawn_monitored("app.test.process:error_receiver_worker", "app:processes")
    if err then
        return false, "spawn failed: " .. tostring(err)
    end

    -- Send our PID
    process.send(child_pid, "link_to", process.pid())

    -- Wait for child to be ready
    local timeout = time.after("5s")
    local result = channel.select {
        inbox_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= inbox_ch then
        return false, "timeout waiting for child ready"
    end

    -- Now error with specific message
    error("SPECIFIC_ERROR_MESSAGE_12345")
end

return { main = main }
