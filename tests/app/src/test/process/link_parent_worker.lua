-- Worker that spawns linked child then exits
-- When this parent exits, the linked child should receive LINK_DOWN
local function main()
    -- Spawn linked child
    local child_pid, err = process.spawn_linked("app.test.process:long_worker", "app:processes")
    if err then
        return false, "spawn failed: " .. tostring(err)
    end

    -- Exit immediately - child should receive LINK_DOWN
    return true
end

return { main = main }
