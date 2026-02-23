-- SPDX-License-Identifier: MPL-2.0

-- Test: events send
local assert = require("assert2")

local function main()
	local events = require("events")

	local ok, err = events.send("test.system", "test.kind", "/test/path")
	assert.is_nil(err, "send should succeed")
	assert.eq(ok, true, "send should return true")

	return true
end

return { main = main }
