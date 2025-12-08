-- Test: Star topology - parent with 10 children that link TO parent
-- When parent errors, all children should receive LINK_DOWN and die
local assert = require("assert2")
local time = require("time")

local function main()
    -- Spawn the parent worker and monitor it
    local parent_pid, err = process.spawn_monitored("app.test.process:star_parent_worker", "app:processes")
    assert.is_nil(err, "spawn parent no error")
    assert.not_nil(parent_pid, "got parent pid")

    -- Wait for parent to complete (including child spawning and error)
    time.sleep("15s")

    -- Try to send to parent - should fail (process not found)
    local ok, send_err = process.send(parent_pid, "test", "hello")
    if ok then
        return false, "parent should be dead but send succeeded"
    end

    -- Verify error mentions process not found
    if not send_err or not string.find(send_err, "not found") then
        return false, "expected 'not found' error, got: " .. tostring(send_err)
    end

    return true
end

return { main = main }
