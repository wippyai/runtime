-- Test: ctx.get, ctx.all (read-only context)
local assert = require("assert2")
local ctx = require("ctx")

local function main()
-- Test ctx.all returns empty table when no values passed
	local all, err = ctx.all()
	assert.is_nil(err, "all no error")
	assert.eq(type(all), "table", "all returns table")

	-- Test get on nonexistent key returns NOT_FOUND error
	local val, err = ctx.get("nonexistent")
	assert.is_nil(val, "get nonexistent returns nil")
	assert.not_nil(err, "get nonexistent returns error")
	assert.eq(err:kind(), errors.NOT_FOUND, "get nonexistent error kind is NOT_FOUND")

	return true
end

return { main = main }
