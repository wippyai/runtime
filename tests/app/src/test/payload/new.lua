-- Test: payload.new functionality
local assert = require("assert2")

local function main()
-- Test creating payload from string
	local p1 = payload.new("hello")
	assert.not_nil(p1, "payload from string")
	assert.eq(p1:get_format(), payload.format.LUA, "string payload is LUA format")

	-- Test creating payload from number
	local p2 = payload.new(123)
	assert.not_nil(p2, "payload from number")
	assert.eq(p2:get_format(), payload.format.LUA, "number payload is LUA format")

	-- Test creating payload from boolean
	local p3 = payload.new(true)
	assert.not_nil(p3, "payload from boolean")
	assert.eq(p3:get_format(), payload.format.LUA, "boolean payload is LUA format")

	-- Test creating payload from table
	local p4 = payload.new({key = "value", num = 42})
	assert.not_nil(p4, "payload from table")
	assert.eq(p4:get_format(), payload.format.LUA, "table payload is LUA format")

	-- Test creating payload from array
	local p5 = payload.new({1, 2, 3})
	assert.not_nil(p5, "payload from array")
	assert.eq(p5:get_format(), payload.format.LUA, "array payload is LUA format")

	-- Test creating payload from nil
	local p6 = payload.new(nil)
	assert.not_nil(p6, "payload from nil")
	assert.eq(p6:get_format(), payload.format.LUA, "nil payload is LUA format")

	-- Test payload tostring
	local str = tostring(p1)
	assert.ok(string.find(str, "payload", 1, true), "tostring contains 'payload'")
	assert.ok(string.find(str, "lua/any", 1, true), "tostring contains format")

	return true
end

return { main = main }
