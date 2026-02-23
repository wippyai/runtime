-- SPDX-License-Identifier: MPL-2.0

-- Test: Call process as function via funcs.call
local assert = require("assert2")
local funcs = require("funcs")

local function main()
-- Test calling process with default_host as function
	local result, err = funcs.call("app.test.processfunc:echo_process", "test input")
	assert.is_nil(err, "call process no error")
	assert.not_nil(result, "call returns result")
	assert.is_table(result, "result is table")
	assert.eq(result.ok, true, "result ok")
	assert.eq(result.echo, "test input", "result echo matches input")

	-- Test calling without args
	local result2, err2 = funcs.call("app.test.processfunc:echo_process")
	assert.is_nil(err2, "call no args no error")
	assert.not_nil(result2, "call no args returns result")
	assert.eq(result2.echo, "no input", "default value used")

	return true
end

return { main = main }
