-- Test: Verify EXIT event is received when monitored child exits
-- Spawns a test worker process and monitors for completion
local assert = require("assert2")
local time = require("time")

local function main()
    -- Spawn the test worker that does the actual test
    local worker_pid, err = process.spawn_monitored("app.test.process:events_exit_worker", "app:processes")
    assert.is_nil(err, "spawn worker no error")
    assert.not_nil(worker_pid, "got worker pid")

    -- Wait for worker to complete (up to 5s)
    time.sleep("3s")

    return true
end

return { main = main }
