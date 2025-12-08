-- Child that waits for LINK_DOWN event
local time = require("time")

local function main()
    local events_ch = process.events()
    if not events_ch then
        return false, "failed to get events channel"
    end

    -- Wait for LINK_DOWN or timeout
    local timeout = time.after("5s")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel == events_ch then
        local event = result.value
        if event then
            return "received:" .. tostring(event:topic())
        end
        return "received:nil"
    end

    return "timeout"
end

return { main = main }
