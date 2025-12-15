-- Test: events subscribe, send, and receive via channel
local assert = require("assert2")

local function main()
    local events = require("events")
    local time = require("time")

    -- Subscribe to test events
    local sub, err = events.subscribe("test.*")
    assert.is_nil(err, "subscribe should succeed: " .. tostring(err))
    assert.not_nil(sub, "subscription should be returned")
    local ch = sub:channel()
    assert.not_nil(ch, "channel should be returned")

    -- Spawn a coroutine to send event after small delay
    coroutine.spawn(function()
        time.sleep(10) -- 10ms delay
        local ok, err = events.send("test.system", "test.kind", "/test/path", {key = "value"})
        assert.is_nil(err, "send should succeed")
        assert.eq(ok, true, "send should return true")
    end)

    -- Wait for event with timeout
    local result = channel.select({
        {ch, "recv"},
        {time.after(2000), "recv"} -- 2 second timeout
    })

    assert.not_nil(result, "should receive result")
    assert.eq(result.index, 1, "should receive from events channel, not timeout")

    local evt = result.value
    assert.not_nil(evt, "event should not be nil")
    assert.eq(evt.system, "test.system", "event system")
    assert.eq(evt.kind, "test.kind", "event kind")
    assert.eq(evt.path, "/test/path", "event path")
    assert.not_nil(evt.data, "event data should exist")
    assert.eq(evt.data.key, "value", "event data.key")

    return true
end

return { main = main }
