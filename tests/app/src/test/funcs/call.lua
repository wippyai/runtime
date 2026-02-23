-- SPDX-License-Identifier: MPL-2.0

-- Test: funcs.call function
local assert = require("assert2")
local funcs = require("funcs")

local function main()
-- Test funcs module is loaded
	assert.not_nil(funcs, "funcs module loaded")
	assert.not_nil(funcs.call, "funcs.call exists")
	assert.eq(type(funcs.call), "function", "funcs.call is function")

	-- Test funcs.new exists (executor factory)
	assert.not_nil(funcs.new, "funcs.new exists")
	assert.eq(type(funcs.new), "function", "funcs.new is function")

	-- Test calling echo function via funcs.call
	local result, err = funcs.call("app.test.funcs:echo", "test input")
	if err then
		error("funcs.call error: " .. tostring(err))
	end
	assert.is_nil(err, "call echo no error")
	assert.not_nil(result, "call echo returns result: " .. tostring(result) .. " type: " .. type(result))
	assert.eq(result.ok, true, "echo result ok")
	assert.eq(result.echo, "test input", "echo result has input")

	-- Test calling without args
	local result2, err2 = funcs.call("app.test.funcs:echo")
	assert.is_nil(err2, "call echo no args no error")
	assert.not_nil(result2, "call echo no args returns result")
	assert.eq(result2.echo, "no input", "echo default value")

	return true
end

return { main = main }
