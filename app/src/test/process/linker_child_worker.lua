-- Child worker that links TO the parent PID passed via inbox
-- When parent errors/terminates, this process receives LINK_DOWN
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

    -- Wait for parent PID to link to
    local timeout = time.after("5s")
    local result = channel.select {
        inbox_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= inbox_ch then
        return false, "timeout waiting for parent pid"
    end

    local msg = result.value
    if not msg then
        return false, "nil message"
    end

    local parent_pid = msg:payload()
    if not parent_pid then
        return false, "no parent pid in message"
    end

    -- Link TO the parent (child -> parent link)
    local ok, err = process.link(parent_pid)
    if not ok then
        return false, "failed to link: " .. tostring(err)
    end

    -- Notify parent we're ready
    process.send(parent_pid, "ready", process.pid())

    -- Wait for LINK_DOWN from parent
    timeout = time.after("30s")
    result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel == events_ch then
        local event = result.value
        if event then
            local topic = event:topic()
            if topic == process.event.LINK_DOWN then
                return "LINK_DOWN_RECEIVED"
            end
            return false, "expected LINK_DOWN, got: " .. tostring(topic)
        end
    end

    return false, "timeout waiting for LINK_DOWN"
end

return { main = main }
