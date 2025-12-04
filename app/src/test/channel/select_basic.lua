-- Test: Basic select operations
local assert = require("assert2")

local function main()
    -- Test select returns table with channel, value, ok
    local ch1 = channel.new(1)
    local ch2 = channel.new(1)

    ch1:send("ch1_value")

    local result = channel.select{
        ch1:case_receive(),
        ch2:case_receive()
    }

    assert.is_table(result, "select returns table")
    assert.not_nil(result.channel, "result has channel")
    assert.eq(result.channel, ch1, "result.channel is ch1")
    assert.eq(result.value, "ch1_value", "result.value correct")
    assert.eq(result.ok, true, "result.ok is true")

    -- Test select with second channel ready
    local ch3 = channel.new(1)
    local ch4 = channel.new(1)

    ch4:send("ch4_value")

    local result2 = channel.select{
        ch3:case_receive(),
        ch4:case_receive()
    }

    assert.eq(result2.channel, ch4, "result2.channel is ch4")
    assert.eq(result2.value, "ch4_value", "result2.value correct")

    -- Test select with case_send on buffered channel
    local ch5 = channel.new(1)
    local result3 = channel.select{ch5:case_send("sent")}

    assert.eq(result3.ok, true, "send result ok")

    local v = ch5:receive()
    assert.eq(v, "sent", "value was sent")

    -- Test case_receive and case_send methods exist
    local ch6 = channel.new(0)
    local case_r = ch6:case_receive()
    local case_s = ch6:case_send("test")

    assert.not_nil(case_r, "case_receive returns object")
    assert.not_nil(case_s, "case_send returns object")

    return true
end

return { main = main }
