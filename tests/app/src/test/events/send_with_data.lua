-- Test: events send with data payload
local assert = require("assert2")

local function main()
	local events = require("events")

	local ok, err = events.send("test.system", "test.kind", "/test/path", {
		key = "value",
		number = 42,
		nested = {foo = "bar"}
	})
	assert.is_nil(err, "send with data should succeed")
	assert.eq(ok, true, "send should return true")

	return true
end

return { main = main }
