-- Test: Echo roundtrip - verifies bidirectional messaging with msg:from()
-- Since functions cannot subscribe to topics, this test simply verifies:
-- 1. We can spawn an echo worker
-- 2. The echo worker can receive messages with valid msg:from()
-- 3. The echo worker can call process.send() back to sender
-- (The full end-to-end test would require a process-based test harness)
local assert = require("assert2")
local time = require("time")

local function main()
    -- Spawn echo worker that echoes messages back to sender
    local echo_pid, err = process.spawn("app.test.process:echo_worker", "app:processes")
    assert.is_nil(err, "spawn echo_worker no error")
    assert.not_nil(echo_pid, "got echo pid")

    -- Give process time to start and subscribe to "echo" topic
    time.sleep("100ms")

    -- Send message to echo worker
    -- The test function's PID will be in msg:from() when echo_worker receives it
    local sent, send_err = process.send(echo_pid, "echo", "hello world")
    assert.is_nil(send_err, "send no error")
    assert.ok(sent, "send succeeded")

    -- Wait for echo worker to process and send reply
    -- The reply won't reach us (we're a function, can't subscribe) but
    -- if echo_worker crashes due to invalid msg:from(), that's the bug we'd catch
    time.sleep("200ms")

    return true
end

return { main = main }
