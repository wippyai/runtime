-- SPDX-License-Identifier: MPL-2.0

-- Test: funcs.async function
local assert = require("assert2")
local funcs = require("funcs")

local function main()
-- Test funcs.async exists
	assert.not_nil(funcs.async, "funcs.async exists")
	assert.eq(type(funcs.async), "function", "funcs.async is function")

	-- Test async call returns a Future
	local future, err = funcs.async("app.test.funcs:echo", "async test")
	assert.is_nil(err, "funcs.async no error")
	assert.not_nil(future, "funcs.async returns future")

	-- Test Future has cancel method
	assert.not_nil(future.cancel, "future has cancel method")
	assert.eq(type(future.cancel), "function", "future.cancel is function")

	-- Test Future has channel method
	assert.not_nil(future.channel, "future has channel method")
	assert.eq(type(future.channel), "function", "future.channel is function")

	-- Test receiving from response channel returns (payload, ok)
	local result, ok = future:response():receive()
	assert.eq(ok, true, "channel receive ok")
	assert.not_nil(result, "channel receive returns payload")
	local data = result:data()
	assert.eq(data.ok, true, "async result ok")
	assert.eq(data.echo, "async test", "async result has input")

	-- Test multiple concurrent async calls
	local f1 = funcs.async("app.test.funcs:echo", "first")
	local f2 = funcs.async("app.test.funcs:echo", "second")
	local f3 = funcs.async("app.test.funcs:echo", "third")

	local p1 = f1:response():receive()
	local p2 = f2:response():receive()
	local p3 = f3:response():receive()

	assert.eq(p1:data().echo, "first", "first async result")
	assert.eq(p2:data().echo, "second", "second async result")
	assert.eq(p3:data().echo, "third", "third async result")

	-- Test executor-based async
	local exec = funcs.new()
	assert.not_nil(exec, "executor created")

	local ef, eerr = exec:async("app.test.funcs:echo", "executor async")
	assert.is_nil(eerr, "executor async no error")
	assert.not_nil(ef, "executor async returns future")

	local ep = ef:response():receive()
	local er = ep:data()
	assert.eq(er.ok, true, "executor async result ok")
	assert.eq(er.echo, "executor async", "executor async result has input")

	return true
end

return { main = main }
