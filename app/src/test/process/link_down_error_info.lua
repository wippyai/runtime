-- Test: Verify process that calls error() dies properly
local assert = require("assert2")
local time = require("time")
local process = require("process")

local function main()
    -- Spawn a worker that immediately errors
    local worker_pid, err = process.spawn_monitored("app.test.process:error_exit_worker", "app:processes")
    assert.is_nil(err, "spawn worker no error")
    assert.not_nil(worker_pid, "got worker pid")

    -- Wait for worker to error
    time.sleep("500ms")

    -- Verify worker is dead
    local ok, send_err = process.send(worker_pid, "test", "hello")
    if ok then
        return false, "worker should be dead but send succeeded"
    end

    if not send_err or not string.find(send_err, "not found") then
        return false, "expected 'not found' error, got: " .. tostring(send_err)
    end

    return true
end

return { main = main }
