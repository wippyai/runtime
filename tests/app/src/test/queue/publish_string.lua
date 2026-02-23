-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

local function main()
	local queue = require("queue")

	-- Test publishing string data
	local ok, err = queue.publish("test:queue", "simple string message")

	-- Without a real queue, this should fail with INVALID (no manager)
	-- or INTERNAL (queue not found). The point is it accepts string data.
	assert.is_nil(ok, "without queue manager should return nil")
	assert.not_nil(err, "should return error without queue manager")

	return true
end

return { main = main }
