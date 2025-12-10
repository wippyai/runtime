-- Worker that links to parent and captures the LINK_DOWN event details
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

    -- Wait for parent PID
    local timeout = time.after("5s")
    local result = channel.select {
        inbox_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= inbox_ch then
        return false, "timeout waiting for parent"
    end

    local msg = result.value
    local parent_pid = msg:payload()

    -- Link to parent
    local ok, err = process.link(parent_pid)
    if not ok then
        return false, "link failed: " .. tostring(err)
    end

    -- Notify ready
    process.send(parent_pid, "ready", "linked")

    -- Wait for LINK_DOWN and capture all details
    timeout = time.after("10s")
    result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= events_ch then
        return false, "timeout waiting for event"
    end

    local event = result.value
    if not event then
        return false, "nil event"
    end

    local topic = event.kind
    if topic ~= process.event.LINK_DOWN then
        return false, "expected LINK_DOWN, got: " .. tostring(topic)
    end

    -- Extract event details
    local from_pid = event.from
    local payload = event:payload()

    -- Verify from is the parent
    if from_pid ~= parent_pid then
        return false, "expected from=" .. parent_pid .. ", got: " .. tostring(from_pid)
    end

    -- Return event details for verification
    return {
        topic = topic,
        from = from_pid,
        has_payload = payload ~= nil,
        payload_kind = payload and payload.kind or nil,
        has_result = payload and payload.result ~= nil or false,
    }
end

return { main = main }
