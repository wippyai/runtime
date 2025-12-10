-- Chain worker: receives depth, spawns next in chain if depth > 0
-- Links to parent (passed via inbox), waits for LINK_DOWN
local function main()
    local events_ch = process.events()
    local inbox_ch = process.inbox()

    -- Wait for setup message from parent (blocking)
    local msg = inbox_ch:receive()
    if not msg or msg:topic() ~= "setup" then
        return false, "expected setup message"
    end

    local payload = msg:payload()
    local link_to = payload.link_to
    local root_pid = payload.root_pid
    local depth = payload.depth or 0

    -- Link to parent
    local ok, err = process.link(link_to)
    if not ok then
        return false, "failed to link to parent: " .. tostring(err)
    end

    -- If depth > 0, spawn next in chain
    if depth > 0 then
        local child_pid
        child_pid, err = process.spawn_monitored("app.test.process:chain_worker", "app:processes")
        if err then
            return false, "spawn child failed: " .. tostring(err)
        end

        -- Send setup to child - link to us, but report to root
        process.send(child_pid, "setup", {
            link_to = process.pid(),
            root_pid = root_pid,
            depth = depth - 1,
        })
    end

    -- Notify root we're ready
    process.send(root_pid, "chain_ready", {
        my_pid = process.pid(),
        depth = depth,
    })

    -- Wait for events (LINK_DOWN from parent) - blocking
    local event = events_ch:receive()

    if event.kind == process.event.LINK_DOWN then
        return "LINK_DOWN:" .. tostring(depth)
    elseif event.kind == process.event.EXIT then
        return "CHILD_EXIT:" .. tostring(depth)
    end

    return false, "unexpected event at depth " .. depth .. ": " .. tostring(event.kind)
end

return { main = main }
