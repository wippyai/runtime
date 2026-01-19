-- Chain worker: receives depth, spawns next in chain if depth > 0
-- Links to parent (passed via inbox), waits for LINK_DOWN
local function main()
    -- Enable trap_links to receive LINK_DOWN events
    local ok, err = process.set_options({ trap_links = true })
    if not ok then
        return false, "set_options failed: " .. tostring(err)
    end

    local events_ch = process.events()
    local inbox_ch = process.inbox()

    -- Wait for setup message from parent (blocking)
    local msg = inbox_ch:receive()
    if not msg or msg:topic() ~= "setup" then
        return false, "expected setup message"
    end

    local payload = msg:payload():data()
    local link_to = string(payload.link_to)
    local root_pid = string(payload.root_pid)
    local depth = tonumber(payload.depth) or 0

    -- Link to parent
    local ok, err = process.link(link_to)
    if not ok then
        return false, "failed to link to parent: " .. tostring(err)
    end

    -- If depth > 0, spawn next in chain
    if depth > 0 then
        local child_pid, spawn_err = process.spawn_monitored("app.test.process:chain_worker", "app:processes")
        if spawn_err or not child_pid then
            return false, "spawn child failed: " .. tostring(spawn_err)
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
