-- Test: Input validation for funcs
local assert = require("assert2")
local funcs = require("funcs")

local function main()
-- Test empty target
	local result, err = funcs.call("")
	assert.is_nil(result, "empty target returns nil")
	assert.not_nil(err, "empty target returns error")

	-- Test target without namespace
	result, err = funcs.call("nonamespace")
	assert.is_nil(result, "no namespace returns nil")
	assert.not_nil(err, "no namespace returns error")

	-- Test async with empty target
	local future, async_err = funcs.async("")
	assert.is_nil(future, "async empty target returns nil")
	assert.not_nil(async_err, "async empty target returns error")

	-- Test async with no namespace
	future, async_err = funcs.async("nonamespace")
	assert.is_nil(future, "async no namespace returns nil")
	assert.not_nil(async_err, "async no namespace returns error")

	-- Test executor call with invalid target
	local exec = funcs.new()
	local exec_result, exec_err = exec:call("")
	assert.is_nil(exec_result, "executor empty target returns nil")
	assert.not_nil(exec_err, "executor empty target returns error")

	return true
end

return { main = main }
