local assert = require("assert2")

local function main()
	local queue = require("queue")

	-- Test error for empty queue ID
	local ok, err = queue.publish("", {data = "test"})
	assert.is_nil(ok, "empty queue ID should return nil")
	assert.not_nil(err, "empty queue ID should return error")
	assert.eq(err:kind(), errors.INVALID, "empty queue ID error kind should be INVALID")
	assert.eq(err:retryable(), false, "error should not be retryable")

	-- Test error for empty data table
	ok, err = queue.publish("test:queue", {})
	assert.is_nil(ok, "empty data should return nil")
	assert.not_nil(err, "empty data should return error")
	assert.eq(err:kind(), errors.INVALID, "empty data error kind should be INVALID")

	-- Test error for message without delivery
	local msg, err2 = queue.message()
	assert.is_nil(msg, "message without delivery should return nil")
	assert.not_nil(err2, "message without delivery should return error")
	assert.eq(err2:kind(), errors.INVALID, "no delivery error kind should be INVALID")

	return true
end

return { main = main }
