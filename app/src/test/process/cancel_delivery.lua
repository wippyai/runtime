-- Test: Verify CANCEL event is properly delivered
local assert = require("assert2")
local time = require("time")

local function main()
    -- Spawn worker that will receive cancel and handle it
    local worker_pid, err = process.spawn_monitored("app.test.process:cancel_receiver_worker", "app:processes")
    assert.is_nil(err, "spawn worker no error")
    assert.not_nil(worker_pid, "got worker pid")

    -- Send setup message
    process.send(worker_pid, "setup", process.pid())

    -- Give worker time to start
    time.sleep("100ms")

    -- Cancel the worker with 3 second timeout
    local ok, cancel_err = process.cancel(worker_pid, "3s")
    assert.is_nil(cancel_err, "cancel no error")

    -- Wait for worker to handle cancel and exit
    time.sleep("5s")

    -- Verify worker is dead
    ok, err = process.send(worker_pid, "test", "hello")
    if ok then
        return false, "worker should be dead but send succeeded"
    end

    if not err or not string.find(err, "not found") then
        return false, "expected 'not found' error, got: " .. tostring(err)
    end

    return true
end

return { main = main }
