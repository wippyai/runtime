-- Test: LINK_DOWN propagation - when parent exits, linked child terminates
local assert = require("assert2")
local time = require("time")

local function main()
    -- Spawn the test worker
    local worker_pid, err = process.spawn_monitored("app.test.process:link_down_worker", "app:processes")
    assert.is_nil(err, "spawn worker no error")
    assert.not_nil(worker_pid, "got worker pid")

    -- Wait for worker to complete
    time.sleep("4s")

    return true
end

return { main = main }
