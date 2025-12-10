-- Child worker that links TO the parent PID passed via inbox
-- When parent errors/terminates, this process receives LINK_DOWN
local function main()
    local events_ch = process.events()
    local inbox_ch = process.inbox()

    -- Wait for parent PID to link to (blocking)
    local msg = inbox_ch:receive()
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

    -- Wait for LINK_DOWN from parent (blocking)
    local event = events_ch:receive()

    if event.kind == process.event.LINK_DOWN then
        return "LINK_DOWN_RECEIVED"
    end

    return false, "expected LINK_DOWN, got: " .. tostring(event.kind)
end

return { main = main }
