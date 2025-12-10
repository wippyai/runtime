-- Test: Verify CANCEL is properly received and handled by child
local assert = require("assert2")
local time = require("time")

local function main()
    local events_ch = process.events()

    local worker_pid, err = process.spawn_monitored("app.test.process:verify_cancel_worker", "app:processes")
    assert.is_nil(err, "spawn worker no error")
    assert.not_nil(worker_pid, "got worker pid")

    -- Worker does: spawn child, cancel child, wait for EXIT
    local timeout = time.after("5s")

    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= events_ch then
        return false, "timeout waiting for worker exit"
    end

    local event = result.value
    if event.kind ~= process.event.EXIT then
        return false, "expected EXIT event, got: " .. tostring(event.kind)
    end

    if event.from ~= worker_pid then
        return false, "expected event from worker " .. worker_pid .. ", got: " .. tostring(event.from)
    end

    return true
end

return { main = main }
