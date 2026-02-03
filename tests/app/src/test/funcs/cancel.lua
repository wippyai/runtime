-- Test: Future cancellation
local assert = require("assert2")
local funcs = require("funcs")

local function main()
-- Start a slow operation
	local future = funcs.async("app.test.funcs:slow", 500, "cancel-test")
	assert.not_nil(future, "future created")

	-- Cancel should exist
	assert.not_nil(future.cancel, "cancel method exists")
	assert.eq(type(future.cancel), "function", "cancel is function")

	-- Cancel the future
	local _, _ = future:cancel()
	-- Cancel may or may not succeed depending on timing
	-- The important thing is it doesn't error unexpectedly

	return true
end

return { main = main }
