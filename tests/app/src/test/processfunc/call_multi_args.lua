-- Test: Process with multiple arguments
local assert = require("assert2")
local funcs = require("funcs")

local function main()
-- Call with multiple arguments
	local result, err = funcs.call("app.test.processfunc:multi_arg_process", "first", "second", "third")
	assert.is_nil(err, "call no error")
	assert.not_nil(result, "result returned")
	assert.eq(result.count, 3, "3 args received")
	assert.eq(result.args.a, "first", "first arg")
	assert.eq(result.args.b, "second", "second arg")
	assert.eq(result.args.c, "third", "third arg")

	-- Call with partial args
	local result2, err2 = funcs.call("app.test.processfunc:multi_arg_process", "only one")
	assert.is_nil(err2, "partial args no error")
	assert.eq(result2.count, 1, "1 arg received")
	assert.eq(result2.args.a, "only one", "first arg set")

	return true
end

return { main = main }
