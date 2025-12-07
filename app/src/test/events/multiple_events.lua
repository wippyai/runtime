-- Test: receive multiple events
local assert = require("assert2")

local function main()
    local events = require("events")
    local time = require("time")

    local ch, err = events.subscribe("test.multi")
    assert.is_nil(err, "subscribe should succeed")

    -- Send 3 events
    coroutine.spawn(function()
        time.sleep(10)
        events.send("test.multi", "kind", "/path1")
        time.sleep(5)
        events.send("test.multi", "kind", "/path2")
        time.sleep(5)
        events.send("test.multi", "kind", "/path3")
    end)

    local received = {}
    for i = 1, 3 do
        local timer = time.after(500)
        local result = channel.select({
            {ch, "recv"},
            {timer, "recv"}
        })
        assert.eq(result.index, 1, "should receive event " .. i)
        table.insert(received, result.value.path)
    end

    assert.eq(#received, 3, "should receive 3 events")
    assert.eq(received[1], "/path1", "first event path")
    assert.eq(received[2], "/path2", "second event path")
    assert.eq(received[3], "/path3", "third event path")

    return true
end

return { main = main }
