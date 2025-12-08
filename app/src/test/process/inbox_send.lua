-- Test: Send message to process via custom topic
local assert = require("assert2")
local process = require("process")
local time = require("time")

local function main()
    -- Test send and listen exist
    assert.not_nil(process.send, "process.send exists")
    assert.not_nil(process.listen, "process.listen exists")

    -- Spawn worker that listens on a custom topic
    local child_pid, err = process.spawn("app.test.process:listen_worker", "app:processes")
    assert.is_nil(err, "spawn listen_worker no error")
    assert.not_nil(child_pid, "got child pid")

    -- Give process time to start and subscribe
    time.sleep("50ms")

    -- Send message to child on custom topic
    local sent, send_err = process.send(child_pid, "messages", "hello from parent")
    assert.is_nil(send_err, "send no error")
    assert.ok(sent, "send succeeded")

    -- Give process time to receive and exit
    time.sleep("100ms")

    return true
end

return { main = main }
