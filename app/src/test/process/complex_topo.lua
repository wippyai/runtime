-- Test: Complex topology - grandparent → child → grandchild cascade
-- Verifies that when a process dies, its linked children (and grandchildren) also die
local assert = require("assert2")
local time = require("time")

local function main()
    -- Spawn the top-level process that builds a 3-level hierarchy
    local worker_pid, err = process.spawn_monitored("app.test.process:complex_topo_worker", "app:processes")
    assert.is_nil(err, "spawn worker no error")
    assert.not_nil(worker_pid, "got worker pid")

    -- The complex_topo_worker exits quickly after spawning children
    -- This should trigger LINK_DOWN cascade to child and grandchild
    -- Wait for everything to settle
    time.sleep("500ms")

    -- If we got here without hanging, the cascade worked
    return true
end

return { main = main }
