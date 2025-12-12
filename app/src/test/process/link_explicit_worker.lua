-- Worker that explicitly links to a target PID
local time = require("time")

local function main()
    local events_ch = process.events()
    local inbox_ch = process.inbox()

    -- Wait for target PID
    local msg = inbox_ch:receive()
    if not msg then
        return false, "nil message"
    end

    local target_pid = msg:payload()
    if not target_pid then
        return false, "no target pid"
    end

    -- Explicitly link to target
    local ok, err = process.link(target_pid)
    if not ok then
        return false, "link failed: " .. tostring(err)
    end

    -- Notify we're linked
    process.send(target_pid, "linked", process.pid())

    -- Wait for LINK_DOWN
    local timeout = time.after("3s")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel == events_ch and result.value.kind == process.event.LINK_DOWN then
        return "LINK_DOWN"
    end

    return false, "expected LINK_DOWN"
end

return { main = main }
