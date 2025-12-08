-- Worker: Long-running process that waits for cancel
local time = require("time")

local function main()
    -- Get events channel to receive cancel signal
    local events_ch = process.events()

    -- Wait for cancel event (or timeout after 10s)
    local timeout = time.after("10s")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel == events_ch then
        local event = result.value
        if event and event:topic() == process.event.CANCEL then
            return "cancelled"
        end
    end

    return "timeout"
end

return { main = main }
