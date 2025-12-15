-- Test: inbox receives only messages that don't match any listener
-- Expected behavior:
-- 1. Messages to topics with listeners go to those listeners
-- 2. Messages to topics without listeners fall through to inbox
-- 3. Inbox acts as catch-all for unmatched topics

local assert = require("assert2")
local time = require("time")

local function main()
    local events_ch = process.events()

    -- Spawn worker that has both inbox and a specific topic listener
    local worker_pid, err = process.spawn_monitored(
        "app.test.process:inbox_fallback_worker",
        "app:processes"
    )
    assert.is_nil(err, "spawn worker no error")
    assert.not_nil(worker_pid, "got worker pid")

    -- Give worker time to set up listeners
    time.sleep("200ms")

    -- Send message to specific topic (should go to listener, NOT inbox)
    local ok1 = process.send(worker_pid, "specific_topic", {
        msg_type = "specific",
        value = 1
    })
    assert.ok(ok1, "send to specific_topic succeeded")

    -- Send message to unregistered topic (should go to inbox)
    local ok2 = process.send(worker_pid, "random_topic", {
        msg_type = "random",
        value = 2
    })
    assert.ok(ok2, "send to random_topic succeeded")

    -- Send another to specific topic
    local ok3 = process.send(worker_pid, "specific_topic", {
        msg_type = "specific",
        value = 3
    })
    assert.ok(ok3, "send second to specific_topic succeeded")

    -- Wait for worker to process and exit
    local timeout = time.after("3s")
    local result = channel.select({
        events_ch:case_receive(),
        timeout:case_receive()
    })

    assert.ok(result.channel ~= timeout, "worker exited before timeout")

    local event = result.value
    assert.eq(event.kind, process.event.EXIT, "got EXIT event")
    if event.result.error ~= nil then
        return false, "worker failed: error=" .. tostring(event.result.error)
    end

    -- Worker returns counts: {specific_count, inbox_count}
    local counts = event.result.value
    assert.not_nil(counts, "got result from worker")
    assert.eq(counts.specific_count, 2, "specific_topic listener got 2 messages")
    assert.eq(counts.inbox_count, 1, "inbox got 1 message (the random_topic one)")

    return true
end

return { main = main }
