-- Test: Send cancel to child and verify it handles cancel properly
-- Spawns a test worker process
local assert = require("assert2")
local time = require("time")

local function main()
    -- Spawn the test worker that does the actual test
    local worker_pid, err = process.spawn_monitored("app.test.process:events_cancel_worker", "app:processes")
    assert.is_nil(err, "spawn worker no error")
    assert.not_nil(worker_pid, "got worker pid")

    -- Wait for worker to complete (up to 5s)
    time.sleep("4s")

    return true
end

return { main = main }
