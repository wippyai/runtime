-- Chain worker: receives depth, spawns next in chain if depth > 0
-- Links to parent (passed via inbox), waits for LINK_DOWN
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

    -- Wait for setup message from parent
    local timeout = time.after("5s")
    local result = channel.select {
        inbox_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= inbox_ch then
        return false, "timeout waiting for setup"
    end

    local msg = result.value
    if not msg or msg:topic() ~= "setup" then
        return false, "expected setup message"
    end

    local payload = msg:payload()
    local parent_pid = payload.parent_pid
    local depth = payload.depth or 0

    -- Link to parent
    local ok, err = process.link(parent_pid)
    if not ok then
        return false, "failed to link to parent: " .. tostring(err)
    end

    -- If depth > 0, spawn next in chain
    local child_pid = nil
    if depth > 0 then
        child_pid, err = process.spawn_monitored("app.test.process:chain_worker", "app:processes")
        if err then
            return false, "spawn child failed: " .. tostring(err)
        end

        -- Send setup to child
        process.send(child_pid, "setup", {
            parent_pid = process.pid(),
            depth = depth - 1,
        })
    end

    -- Notify original parent we're ready
    process.send(parent_pid, "chain_ready", {
        my_pid = process.pid(),
        depth = depth,
    })

    -- Wait for events (LINK_DOWN from parent)
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
                return "LINK_DOWN:" .. tostring(depth)
            elseif topic == process.event.EXIT then
                -- Child exited - that's also fine
                return "CHILD_EXIT:" .. tostring(depth)
            end
            return false, "unexpected event at depth " .. depth .. ": " .. tostring(topic)
        end
    end

    return false, "timeout at depth " .. tostring(depth)
end

return { main = main }
