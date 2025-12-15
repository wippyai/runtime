-- Test: Inbox messages arrive in order
local assert = require("assert2")
local time = require("time")

local function main()
    local events_ch = process.events()

    -- Spawn worker that receives multiple messages and verifies order
    local worker_pid, err = process.spawn_monitored("app.test.process:inbox_order_worker", "app:processes")
    assert.is_nil(err, "spawn worker no error")
    assert.not_nil(worker_pid, "got worker pid")

    -- Send 5 messages immediately - should be queued in order
    for i = 1, 5 do
        local ok, send_err = process.send(worker_pid, "inbox", i)
        assert.is_nil(send_err, "send " .. i .. " no error")
    end

    -- Wait for worker to exit
    local timeout = time.after("5s")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= events_ch then
        return false, "timeout waiting for worker EXIT"
    end

    local event = result.value
    assert.eq(event.kind, process.event.EXIT, "got EXIT event")

    -- Check worker didn't fail
    if event.error then
        return false, "worker failed: " .. tostring(event.error)
    end

    return true
end

return { main = main }
