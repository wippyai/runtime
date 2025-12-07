-- Test: events subscribe returns channel
local assert = require("assert2")

local function main()
    local events = require("events")

    local ch, err = events.subscribe("test.*")
    assert.is_nil(err, "subscribe should succeed")
    assert.not_nil(ch, "channel should be returned")

    return true
end

return { main = main }
