-- Test: events subscribe with kind filter
local assert = require("assert2")

local function main()
    local events = require("events")

    local sub, err = events.subscribe("test.system", "test.kind")
    assert.is_nil(err, "subscribe with kind should succeed")
    local ch = sub:channel()
    assert.not_nil(ch, "channel should be returned")

    return true
end

return { main = main }
