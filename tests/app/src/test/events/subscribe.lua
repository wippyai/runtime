-- Test: events subscribe returns subscription with channel
local assert = require("assert2")

local function main()
	local events = require("events")

	local sub, err = events.subscribe("test.*")
	assert.is_nil(err, "subscribe should succeed")
	assert.not_nil(sub, "subscription should be returned")
	assert.not_nil(sub.channel, "subscription should have channel method")

	local ch = sub:channel()
	assert.not_nil(ch, "channel should be returned")

	return true
end

return { main = main }
