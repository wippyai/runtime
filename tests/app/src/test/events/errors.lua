-- SPDX-License-Identifier: MPL-2.0

-- Test: events error handling
local assert = require("assert2")

local function main()
	local events = require("events")

	-- Empty system should fail
	local sub, err = events.subscribe("")
	assert.is_nil(sub, "empty system should return nil subscription")
	assert.not_nil(err, "empty system should return error")

	-- Send with empty system should fail
	local ok, err = events.send("", "kind", "/path")
	assert.is_nil(ok, "empty system should return nil")
	assert.not_nil(err, "empty system should return error")

	-- Send with empty kind should fail
	ok, err = events.send("system", "", "/path")
	assert.is_nil(ok, "empty kind should return nil")
	assert.not_nil(err, "empty kind should return error")

	-- Send with empty path should fail
	ok, err = events.send("system", "kind", "")
	assert.is_nil(ok, "empty path should return nil")
	assert.not_nil(err, "empty path should return error")

	return true
end

return { main = main }
