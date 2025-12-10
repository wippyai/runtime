-- Tests that LINK_DOWN is sent when parent exits with error
local function main()
    local events_ch = process.events()

    -- Spawn linked child that will wait for events
    local child_pid, err = process.spawn_linked_monitored("app.test.process:linked_child_worker", "app:processes")
    if err then
        return false, "spawn linked child failed: " .. tostring(err)
    end

    -- Spawn linked error worker - when it errors, LINK_DOWN should propagate
    local error_pid, err2 = process.spawn_linked("app.test.process:error_exit_worker", "app:processes")
    if err2 then
        return false, "spawn error worker failed: " .. tostring(err2)
    end

    -- Wait for LINK_DOWN event (blocking)
    local event = events_ch:receive()

    if event.kind == process.event.LINK_DOWN then
        return true
    end

    return false, "expected LINK_DOWN, got: " .. tostring(event.kind)
end

return { main = main }
