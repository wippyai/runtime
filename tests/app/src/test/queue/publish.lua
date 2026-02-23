-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

local function main()
	local queue = require("queue")

	-- publish function exists
	assert.not_nil(queue.publish, "publish function should exist")

	-- publish with empty queue ID should fail
	local ok, err = queue.publish("", { data = "test" })
	assert.is_nil(ok, "publish with empty queue ID should return nil")
	assert.not_nil(err, "publish with empty queue ID should return error")

	-- publish with empty data should fail
	ok, err = queue.publish("test:myqueue", {})
	assert.is_nil(ok, "publish with empty data should return nil")
	assert.not_nil(err, "publish with empty data should return error")

	return true
end

return { main = main }
