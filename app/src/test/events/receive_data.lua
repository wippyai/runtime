-- Test: events receive with data payload
local assert = require("assert2")

local function main()
    local events = require("events")
    local time = require("time")

    local ch, err = events.subscribe("test.data")
    assert.is_nil(err, "subscribe should succeed")

    coroutine.spawn(function()
        time.sleep(10 * time.MILLISECOND)
        events.send("test.data", "test.kind", "/test/path", {
            string_val = "hello",
            number_val = 42,
            bool_val = true,
            nested = {a = 1, b = 2}
        })
    end)

    local timer = time.after(500 * time.MILLISECOND)
    local result = channel.select{
        ch:case_receive(),
        timer:case_receive()
    }

    assert.eq(result.channel, ch, "should receive event")

    local evt = result.value
    assert.not_nil(evt.data, "event should have data")
    assert.eq(evt.data.string_val, "hello", "string value")
    assert.eq(evt.data.number_val, 42, "number value")
    assert.eq(evt.data.bool_val, true, "bool value")
    assert.not_nil(evt.data.nested, "nested table")
    assert.eq(evt.data.nested.a, 1, "nested value a")
    assert.eq(evt.data.nested.b, 2, "nested value b")

    return true
end

return { main = main }
