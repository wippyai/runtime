-- Worker that tests linked process termination
local function main()
    local events_ch = process.events()

    -- Spawn linked and monitored long-running worker
    local child_pid, err = process.spawn_linked_monitored("app.test.process:long_worker", "app:processes")
    if err then
        return false, "spawn failed: " .. tostring(err)
    end

    -- Terminate the child
    local terminated, term_err = process.terminate(child_pid)
    if term_err then
        return false, "terminate failed: " .. tostring(term_err)
    end

    -- Wait for EXIT event (blocking)
    local event = events_ch:receive()

    if event.kind ~= process.event.EXIT then
        return false, "expected EXIT, got: " .. tostring(event.kind)
    end

    return true
end

return { main = main }
