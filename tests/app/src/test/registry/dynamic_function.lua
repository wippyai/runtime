-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local funcs = require("funcs")

local function main()
-- call function dynamically by known ID
	local func_id = "app.test.funcs:echo"
	local result, err = funcs.call(func_id, "dynamic test")
	assert.is_nil(err, "call dynamic function no error")
	assert.not_nil(result, "call returns result")
	assert.eq(result.ok, true, "result ok")
	assert.eq(result.echo, "dynamic test", "echo received input")

	-- call another function to verify dynamic dispatch
	local result2, err2 = funcs.call("app.test.funcs:echo", "second call")
	assert.is_nil(err2, "second call no error")
	assert.eq(result2.echo, "second call", "second call result")

	return true
end

return { main = main }
