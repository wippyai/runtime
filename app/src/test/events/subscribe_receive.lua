-- Test: events subscribe and receive
local assert = require("assert_primitives")

local function main()
    local events = require("events")
    local channel = require("channel")
    local time = require("time")

    -- Subscribe to test events
    local ch, err = events.subscribe("test.*")
    assert.is_nil(err, "subscribe should succeed")
    assert.not_nil(ch, "channel should be returned")

    -- Send an event
    local ok, err = events.send("test.system", "test.kind", "/test/path", {key = "value"})
    assert.is_nil(err, "send should succeed")
    assert.eq(ok, true, "send should return true")

    -- Wait for event with timeout
    local result = channel.select({
        {ch, "recv"},
        {time.after(1000), "recv"}
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
