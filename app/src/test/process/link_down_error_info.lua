-- Test: Verify parent that errors causes child to die with LINK_DOWN
local assert = require("assert2")
local time = require("time")

local function main()
    -- Spawn parent worker that spawns a child and then errors
    local parent_pid, err = process.spawn_monitored("app.test.process:error_parent_worker", "app:processes")
    assert.is_nil(err, "spawn parent no error")
    assert.not_nil(parent_pid, "got parent pid")

    -- Wait for parent to error
    time.sleep("5s")

    -- Verify parent is dead
    local ok, send_err = process.send(parent_pid, "test", "hello")
    if ok then
        return false, "parent should be dead but send succeeded"
    end

    if not send_err or not string.find(send_err, "not found") then
        return false, "expected 'not found' error, got: " .. tostring(send_err)
    end

    return true
end

return { main = main }
