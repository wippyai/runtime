-- SPDX-License-Identifier: MPL-2.0

-- Test: Error propagation through funcs.call
local assert = require("assert2")
local funcs = require("funcs")

local function check_raised(err, kind, message)
	assert.not_nil(err, "raised error returned")
	assert.eq(err:kind(), kind, "raised kind")
	assert.eq(err:message(), message, "raised message")
	assert.eq(err:details().field, "target", "raised detail")
	assert.eq(err:details().nested.code, "required", "raised nested detail")
	assert.eq(errors.is(err, kind), true, "raised errors.is")
end

local function main()
-- Test calling function that returns structured error
	local result, err = funcs.call("app.test.funcs:error_with_kind", errors.INVALID, "invalid input")
	assert.is_nil(result, "result is nil on error")
	assert.not_nil(err, "error returned from call")
	assert.eq(err:kind(), errors.INVALID, "error kind preserved")
	assert.eq(err:retryable(), false, "error retryable preserved")

	-- Test retryable error propagation
	result, err = funcs.call("app.test.funcs:error_with_kind", errors.UNAVAILABLE, "service down")
	assert.not_nil(err, "error returned")
	assert.eq(err:kind(), errors.UNAVAILABLE, "UNAVAILABLE kind")
	assert.eq(err:retryable(), true, "retryable is true")

	-- Test NOT_FOUND propagation
	result, err = funcs.call("app.test.funcs:error_with_kind", errors.NOT_FOUND, "resource missing")
	assert.not_nil(err, "error returned")
	assert.eq(err:kind(), errors.NOT_FOUND, "NOT_FOUND kind")

	-- Test error propagation via executor
	local exec = funcs.new()
	result, err = exec:call("app.test.funcs:error_with_kind", errors.PERMISSION_DENIED, "access denied")
	assert.not_nil(err, "error from executor call")
	assert.eq(err:kind(), errors.PERMISSION_DENIED, "PERMISSION_DENIED kind via executor")

	-- Test async error propagation preserves kind
	local future = funcs.async("app.test.funcs:error_with_kind", errors.INTERNAL, "internal failure")
	local aerr, ok = future:response():receive()
	assert.eq(ok, true, "channel ok is true when value received")
	assert.not_nil(aerr, "error from async")
	assert.eq(aerr:kind(), errors.INTERNAL, "INTERNAL kind via async")

	local value
	value, err = funcs.call("app.test.funcs:raise_with_kind", errors.INVALID, "bad declaration", false)
	assert.is_nil(value, "raised call returns nil")
	check_raised(err, errors.INVALID, "bad declaration")
	assert.eq(err:retryable(), false, "raised false retryable")

	value, err = funcs.new():call("app.test.funcs:raise_with_kind", errors.UNAVAILABLE, "bad declaration", true, true)
	assert.is_nil(value, "raised executor call returns nil")
	check_raised(err, errors.UNAVAILABLE, "bad declaration")
	assert.eq(err:retryable(), true, "raised true retryable")

	value, err = funcs.call("app.test.funcs:raise_with_kind", errors.INVALID, "bad declaration")
	assert.is_nil(value, "raised call with unspecified retry returns nil")
	check_raised(err, errors.INVALID, "bad declaration")
	assert.is_nil(err:retryable(), "raised retryable remains unspecified")

	value, err = funcs.call("app.test.funcs:raise_on_load")
	assert.is_nil(value, "failed initialization returns nil")
	check_raised(err, errors.INVALID, "bad declaration")
	assert.eq(err:retryable(), false, "load retryable")

	value, err = funcs.call("app.test.funcs:reraise_with_kind")
	assert.is_nil(value, "nested raise returns nil")
	check_raised(err, errors.INVALID, "bad declaration")
	assert.eq(err:retryable(), false, "nested retryable")
	value, err = funcs.call("app.test.funcs:reraise_with_kind", true)
	assert.is_nil(value, "nested return returns nil")
	check_raised(err, errors.INVALID, "bad declaration")
	assert.eq(err:retryable(), false, "nested returned retryable")

	local raised_future = funcs.async("app.test.funcs:raise_with_kind", errors.INTERNAL, "bad declaration", true)
	local async_err, received = raised_future:response():receive()
	assert.eq(received, true, "raised async response received")
	check_raised(async_err, errors.INTERNAL, "bad declaration")
	assert.eq(async_err:retryable(), true, "async retryable")

	local ok, local_err = pcall(function()
		error(errors.new({ message = "local error", kind = errors.INVALID, retryable = false }))
	end)
	assert.eq(ok, false, "pcall catches typed error")
	assert.eq(local_err:kind(), errors.INVALID, "pcall keeps native kind")

	value, err = funcs.call("app.test.funcs:raise_with_kind", "plain", "bad declaration")
	assert.is_nil(value, "plain raise returns nil")
	assert.not_nil(err, "plain raise is an error")
	assert.eq(err:kind(), errors.INTERNAL, "plain string remains Internal")
	assert.eq(err:retryable(), false, "plain string is not retryable")

	return true
end

return { main = main }
