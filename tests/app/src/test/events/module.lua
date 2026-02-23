-- SPDX-License-Identifier: MPL-2.0

-- Test: events module basic checks
local assert = require("assert2")

local function main()
	local events = require("events")

	assert.not_nil(events, "events module should load")
	assert.not_nil(events.subscribe, "subscribe function should exist")
	assert.not_nil(events.send, "send function should exist")

	return true
end

return { main = main }
