-- SPDX-License-Identifier: MPL-2.0

-- Test: Future error handling
local assert = require("assert2")
local funcs = require("funcs")

local function main()
-- Test async call that returns error
	local future = funcs.async("app.test.funcs:error_func", true, "async error test")
	assert.not_nil(future, "future created for error case")

	-- Receive returns error payload
	local result, ok = future:response():receive()
	assert.eq(ok, true, "channel ok")
	-- On error, result is an error object
	assert.not_nil(result, "error payload returned")

	-- After error completion, is_complete is true
	local complete = future:is_complete()
	assert.eq(complete, true, "future complete after error")

	-- result() returns (nil, error) on error
	local val, rerr = future:result()
	assert.is_nil(val, "result() returns nil value on error")
	assert.not_nil(rerr, "result() returns error on error")

	-- error() returns (error, ok) - when error exists ok is true
	local ferr, eok = future:error()
	assert.eq(eok, true, "error() returns true when error exists")
	assert.not_nil(ferr, "error() returns error object")

	return true
end

return { main = main }
