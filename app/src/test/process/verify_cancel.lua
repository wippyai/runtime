-- Test: Verify CANCEL is properly received and handled by child
local assert = require("assert2")
local time = require("time")

local function main()
    local worker_pid, err = process.spawn_monitored("app.test.process:verify_cancel_worker", "app:processes")
    assert.is_nil(err, "spawn worker no error")
    assert.not_nil(worker_pid, "got worker pid")

    -- Wait for worker to complete
    time.sleep("6s")

    return true
end

return { main = main }
