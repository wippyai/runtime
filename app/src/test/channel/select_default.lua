-- Test: Select with default case
local assert = require("assert2")

local function main()
    -- Test select.exists
    assert.not_nil(channel.select, "channel.select exists")

    -- Test select with default when no channels ready
    local ch1 = channel.new(0)
    local ch2 = channel.new(0)

    local result = channel.select{
        ch1:case_receive(),
        ch2:case_receive(),
        default = true
    }

    assert.is_table(result, "select returns table")
    assert.eq(result.default, true, "default case selected")
    assert.eq(result.ok, true, "default ok is true")

    -- Test select picks ready channel over default
    local ch3 = channel.new(1)
    local ch4 = channel.new(1)
    ch3:send("ready")

    local result2 = channel.select{
        ch3:case_receive(),
        ch4:case_receive(),
        default = true
    }

    assert.eq(result2.channel, ch3, "picked ready channel")
    assert.eq(result2.value, "ready", "got value")
    assert.eq(result2.ok, true, "ok is true")
    assert.is_nil(result2.default, "not default")

    return true
end

return { main = main }
