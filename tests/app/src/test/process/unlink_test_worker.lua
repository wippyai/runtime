-- Worker that links then unlinks from target
local time = require("time")

local function main()
    -- Enable trap_links to receive LINK_DOWN events (for verification it doesn't arrive after unlink)
    local ok, err = process.set_options({ trap_links = true })
    if not ok then
        return false, "set_options failed: " .. tostring(err)
    end

    local events_ch = process.events()
    local inbox_ch = process.inbox()

    -- Wait for target PID
    local msg = inbox_ch:receive()
    if not msg then
        return false, "nil message"
    end

    local target_pid = msg:payload():data()
    if not target_pid then
        return false, "no target pid"
    end

    -- Link to target
    local ok, err = process.link(target_pid)
    if not ok then
        return false, "link failed: " .. tostring(err)
    end

    -- Notify linked
    process.send(target_pid, "linked", process.pid())

    -- Wait for unlink command
    msg = inbox_ch:receive()
    if not msg or msg:topic() ~= "unlink" then
        return false, "expected unlink command"
    end

    -- Unlink from target
    ok, err = process.unlink(target_pid)
    if not ok then
        return false, "unlink failed: " .. tostring(err)
    end

    -- Notify unlinked
    process.send(target_pid, "unlinked", process.pid())

    -- Wait a bit for any events (should not get LINK_DOWN now)
    local timeout = time.after("500ms")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel == timeout then
        return "NO_LINK_DOWN"
    end

    -- Got an event when we shouldn't have after unlink
    local event = result.value
    local kind_str = "nil"
    if event then
        kind_str = tostring(event.kind)
    end
    return false, "unexpected event after unlink: kind=" .. kind_str
end

return { main = main }
