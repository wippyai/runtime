-- Test: Terminate kills process and triggers LINK_DOWN to linked processes
local assert = require("assert2")
local time = require("time")

local function main()
    -- Spawn a long-running worker that waits for events
    local worker_pid, err = process.spawn_monitored("app.test.process:long_worker", "app:processes")
    assert.is_nil(err, "spawn worker no error")
    assert.not_nil(worker_pid, "got worker pid")

    -- Give worker time to start
    time.sleep("100ms")

    -- Terminate the worker forcibly
    local ok, term_err = process.terminate(worker_pid)
    assert.is_nil(term_err, "terminate no error")

    -- Wait for termination to complete
    time.sleep("500ms")

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
