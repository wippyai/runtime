-- Test: LINK_DOWN is NOT sent when linked process exits normally
local assert = require("assert2")
local time = require("time")

local function main()
    local worker_pid, err = process.spawn_monitored("app.test.process:verify_no_link_down_on_normal_worker", "app:processes")
    assert.is_nil(err, "spawn worker no error")
    assert.not_nil(worker_pid, "got worker pid")

    time.sleep("2s")
    return true
end

return { main = main }
