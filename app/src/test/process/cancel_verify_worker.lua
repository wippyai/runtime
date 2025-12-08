-- Worker that verifies CANCEL event is received and properly handled
local time = require("time")

local function main()
    local events_ch = process.events()
    if not events_ch then
        return false, "failed to get events channel"
    end

    -- Signal ready by waiting briefly
    time.sleep("10ms")

    -- Wait for CANCEL event
    local timeout = time.after("5s")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= events_ch then
        return false, "timeout waiting for cancel"
    end

    local event = result.value
    if not event then
        return false, "received nil event"
    end

    local topic = event:topic()
    if topic ~= process.event.CANCEL then
        return false, "expected CANCEL, got: " .. tostring(topic)
    end

    -- Return cancelled to signal we handled it
    return "cancelled"
end

return { main = main }
