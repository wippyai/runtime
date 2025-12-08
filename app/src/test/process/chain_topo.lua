-- Test: Chain topology - A -> B -> C -> D -> E -> F
-- Each node links to its parent. When root errors, cascade kills all.
local assert = require("assert2")
local time = require("time")

local function main()
    -- Spawn the chain root
    local root_pid, err = process.spawn_monitored("app.test.process:chain_root_worker", "app:processes")
    assert.is_nil(err, "spawn root no error")
    assert.not_nil(root_pid, "got root pid")

    -- Wait for chain to build and root to error
    time.sleep("15s")

    -- Try to send to root - should fail (process not found)
    local ok, send_err = process.send(root_pid, "test", "hello")
    if ok then
        return false, "root should be dead but send succeeded"
    end

    -- Verify error mentions process not found
    if not send_err or not string.find(send_err, "not found") then
        return false, "expected 'not found' error, got: " .. tostring(send_err)
    end

    return true
end

return { main = main }
