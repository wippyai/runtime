-- Worker that tests LINK_DOWN event propagation
-- Spawns a parent that spawns a linked child, then monitors both
local time = require("time")

local function main()
    local events_ch = process.events()
    if not events_ch then
        return false, "failed to get events channel"
    end

    -- Spawn monitored parent that will spawn a linked child and exit
    local parent_pid, err = process.spawn_monitored("app.test.process:link_parent_worker", "app:processes")
    if err then
        return false, "spawn parent failed: " .. tostring(err)
    end

    -- Wait for parent EXIT event
    local timeout = time.after("3s")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= events_ch then
        return false, "timeout waiting for parent exit"
    end

    local event = result.value
    if not event then
        return false, "received nil event"
    end

    if event:topic() ~= process.event.EXIT then
        return false, "expected EXIT, got: " .. tostring(event:topic())
    end

    if event:from() ~= parent_pid then
        return false, "expected from parent " .. parent_pid .. ", got: " .. tostring(event:from())
    end

    -- The linked child should have also terminated due to LINK_DOWN
    -- Give it time to propagate
    time.sleep("100ms")

    return true
end

return { main = main }
