-- Test: events receive via channel with coroutine
local assert = require("assert2")

local function main()
    local events = require("events")
    local time = require("time")

    -- Subscribe to test events
    local sub, err = events.subscribe("test.system")
    assert.is_nil(err, "subscribe should succeed")
    assert.not_nil(sub, "subscription should be returned")

    local ch = sub:channel()

    -- Spawn sender coroutine
    coroutine.spawn(function()
        time.sleep(10 * time.MILLISECOND)
        events.send("test.system", "test.kind", "/test/path", {key = "value"})
    end)

    -- Wait for event with timeout
    local timer = time.after(500 * time.MILLISECOND)
    local result = channel.select{
        ch:case_receive(),
        timer:case_receive()
    }

    assert.not_nil(result, "should receive result")
    assert.eq(result.channel, ch, "should receive from events channel")

    local evt = result.value
    assert.not_nil(evt, "event should not be nil")
    assert.eq(evt.system, "test.system", "event system")
    assert.eq(evt.kind, "test.kind", "event kind")
    assert.eq(evt.path, "/test/path", "event path")

    return true
end

return { main = main }
