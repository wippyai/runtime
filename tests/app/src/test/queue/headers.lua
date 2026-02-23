-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

local function main()
	local queue = require("queue")

	-- Test publishing with various header types
	local ok, err = queue.publish("test:queue", "data", {
		string_header = "value",
		number_header = 42,
		float_header = 3.14,
		bool_true = true,
		bool_false = false,
		correlation_id = "corr-123",
		priority = 5
	})

	-- Without a real queue, this should fail
	assert.is_nil(ok, "without queue manager should return nil")
	assert.not_nil(err, "should return error without queue manager")

	return true
end

return { main = main }
