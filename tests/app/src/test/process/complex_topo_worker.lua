-- Tests complex topology: grandparent → child → grandchild
-- When grandparent dies, entire tree should die via LINK_DOWN cascade
local time = require("time")

local function main()
    local events_ch = process.events()
    if not events_ch then
        return false, "failed to get events channel"
    end

    -- Spawn monitored and linked child (which will spawn linked grandchild)
    local child_pid, err = process.spawn_linked_monitored("app.test.process:child_with_grandchild_worker", "app:processes")
    if err then
        return false, "spawn child failed: " .. tostring(err)
    end

    -- Give hierarchy time to build
    time.sleep("100ms")

    -- Now terminate ourselves - should cascade to child and grandchild
    -- This is done by simply returning, which will trigger LINK_DOWN to child
    -- and child's LINK_DOWN should cascade to grandchild

    return true
end

return { main = main }
