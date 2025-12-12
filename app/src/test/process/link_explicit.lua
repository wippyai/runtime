-- Test: process.link explicit function
local assert = require("assert2")
local time = require("time")

local function main()
    -- Test link exists
    assert.not_nil(process.link, "process.link exists")
    assert.is_function(process.link, "process.link is function")

    local events_ch = process.events()
    local my_pid = process.pid()

    -- Spawn worker that will link to us
    local worker_pid, err = process.spawn_monitored(
        "app.test.process:link_explicit_worker",
        "app:processes"
    )
    assert.is_nil(err, "spawn worker no error")

    -- Send our PID to worker
    process.send(worker_pid, "inbox", my_pid)

    -- Wait for worker to confirm link
    local inbox_ch = process.inbox()
    local timeout = time.after("2s")
    local result = channel.select {
        inbox_ch:case_receive(),
        timeout:case_receive(),
    }
    assert.neq(result.channel, timeout, "received link confirmation")

    -- Now exit - worker should receive LINK_DOWN
    -- We monitor the worker to verify it exits properly
    timeout = time.after("3s")
    result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= events_ch then
        return false, "timeout waiting for worker exit"
    end

    assert.eq(result.value.kind, process.event.EXIT, "got EXIT from worker")

    return true
end

return { main = main }
