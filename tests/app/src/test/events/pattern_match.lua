-- Test: pattern matching for subscriptions
local assert = require("assert2")

local function main()
    local events = require("events")
    local time = require("time")

    -- Subscribe to test.* pattern (matches test.match, test.also, etc.)
    local sub, err = events.subscribe("test.*")
    assert.is_nil(err, "subscribe should succeed")
    local ch = sub:channel()

    -- Send matching and non-matching events
    coroutine.spawn(function()
        time.sleep(10 * time.MILLISECOND)
        events.send("test.match", "kind", "/matched")
        time.sleep(5 * time.MILLISECOND)
        events.send("other.nomatch", "kind", "/notmatched")
        time.sleep(5 * time.MILLISECOND)
        events.send("test.also", "kind", "/alsomatched")
    end)

    -- Should receive only matching events
    local received = {}
    for i = 1, 2 do
        local timer = time.after(500 * time.MILLISECOND)
        local result = channel.select{
            ch:case_receive(),
            timer:case_receive()
        }
        if result.channel == ch then
            table.insert(received, result.value.path)
        end
    end

    assert.eq(#received, 2, "should receive 2 matching events")
    assert.eq(received[1], "/matched", "first match")
    assert.eq(received[2], "/alsomatched", "second match")

    return true
end

return { main = main }
