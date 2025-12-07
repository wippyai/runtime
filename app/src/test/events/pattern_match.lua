-- Test: pattern matching for subscriptions
local assert = require("assert2")

local function main()
    local events = require("events")
    local time = require("time")

    -- Subscribe to test.* pattern (matches test.match, test.also, etc.)
    local ch, err = events.subscribe("test.*")
    assert.is_nil(err, "subscribe should succeed")

    -- Send matching and non-matching events
    coroutine.spawn(function()
        time.sleep(10)
        events.send("test.match", "kind", "/matched")
        time.sleep(5)
        events.send("other.nomatch", "kind", "/notmatched")
        time.sleep(5)
        events.send("test.also", "kind", "/alsomatched")
    end)

    -- Should receive only matching events
    local received = {}
    for i = 1, 2 do
        local timer = time.after(500)
        local result = channel.select({
            {ch, "recv"},
            {timer, "recv"}
        })
        if result.index == 1 then
            table.insert(received, result.value.path)
        end
    end

    assert.eq(#received, 2, "should receive 2 matching events")
    assert.eq(received[1], "/matched", "first match")
    assert.eq(received[2], "/alsomatched", "second match")

    return true
end

return { main = main }
