-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

local function main()
	local queue = require("queue")

	-- Test publishing table data with nested structures
	local data = {
		user_id = 123,
		action = "purchase",
		items = {"item1", "item2", "item3"},
		metadata = {
			source = "api",
			timestamp = 1234567890,
			nested = {
				level = 2
			}
		}
	}

	local ok, err = queue.publish("test:queue", data)

	-- Without a real queue, this should fail
	assert.is_nil(ok, "without queue manager should return nil")
	assert.not_nil(err, "should return error without queue manager")

	return true
end

return { main = main }
